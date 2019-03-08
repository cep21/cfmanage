package awscache

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/aws/aws-sdk-go/service/sts"
	"github.com/cep21/cfmanage/internal/cleanup"
	"github.com/cep21/cfmanage/internal/logger"
	"github.com/cep21/cfmanage/internal/oncecache"
	"github.com/pkg/errors"
)

type cacheKey struct {
	region  string
	profile string
}

type AWSCache struct {
	Cleanup      *cleanup.Cleanup
	mu           sync.Mutex
	sessionCache map[cacheKey]*AWSClients
}

func (a *AWSCache) Session(profile string, region string) (*AWSClients, error) {
	itemKey := cacheKey{
		region:  region,
		profile: profile,
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.sessionCache[itemKey] != nil {
		return a.sessionCache[itemKey], nil
	}
	cfg := aws.Config{}
	if region != "" {
		cfg.Region = &region
	}
	ses, err := session.NewSessionWithOptions(session.Options{
		Profile: profile,
		Config:  cfg,
	})
	if err != nil {
		return nil, err
	}
	if a.sessionCache == nil {
		a.sessionCache = make(map[cacheKey]*AWSClients)
	}
	a.sessionCache[itemKey] = &AWSClients{
		session: ses,
		cleanup: a.Cleanup,
	}
	return a.sessionCache[itemKey], nil
}

type AWSClients struct {
	session *session.Session
	cleanup *cleanup.Cleanup

	accountID oncecache.StringCache
	myToken   string
	mu        sync.Mutex
}

func (a *AWSClients) token() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.myToken == "" {
		a.myToken = strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return a.myToken
}

func (a *AWSClients) Region() string {
	return *a.session.Config.Region
}

func (a *AWSClients) AccountID() (string, error) {
	return a.accountID.Do(func() (string, error) {
		stsClient := sts.New(a.session)
		out, err := stsClient.GetCallerIdentity(&sts.GetCallerIdentityInput{})
		if err != nil {
			return "", errors.Wrap(err, "unable to fetch identity ID")
		}
		return *out.Account, nil
	})
}

func (a *AWSClients) DescribeStack(ctx context.Context, name string) (*cloudformation.Stack, error) {
	cf := cloudformation.New(a.session)
	res, err := cf.DescribeStacksWithContext(ctx, &cloudformation.DescribeStacksInput{
		StackName: &name,
	})
	if err != nil {
		if strings.Contains(err.Error(), "does not exist") {
			return nil, nil
		}
		return nil, errors.Wrapf(err, "unable to describe stack %s", name)
	}
	if len(res.Stacks) == 0 {
		return nil, nil
	}
	return res.Stacks[0], nil
}

func guessChangesetType(ctx context.Context, cloudformationClient *cloudformation.CloudFormation, in *cloudformation.CreateChangeSetInput) *cloudformation.CreateChangeSetInput {
	if in == nil || in.ChangeSetType == nil {
		return in
	}
	if *in.ChangeSetType != "GUESS" {
		return in
	}
	_, err := cloudformationClient.DescribeStacksWithContext(ctx, &cloudformation.DescribeStacksInput{
		StackName: in.StackName,
	})
	if err != nil {
		// stack does not exist (probably)
		in.ChangeSetType = aws.String("CREATE")
	} else {
		in.ChangeSetType = aws.String("UPDATE")
	}
	return in
}

func (a *AWSClients) CreateChangesetWaitForStatus(ctx context.Context, in *cloudformation.CreateChangeSetInput) (*cloudformation.DescribeChangeSetOutput, error) {
	if in.ChangeSetName == nil {
		in.ChangeSetName = aws.String("A" + strconv.FormatInt(time.Now().UnixNano(), 16))
	}
	in.ClientToken = aws.String(a.token())
	cf := cloudformation.New(a.session)
	in = guessChangesetType(ctx, cf, in)
	res, err := cf.CreateChangeSetWithContext(ctx, in)
	if err != nil {
		if strings.Contains(err.Error(), "AlreadyExistsException") {
			_, err := cf.DeleteChangeSetWithContext(ctx, &cloudformation.DeleteChangeSetInput{
				ChangeSetName: in.ChangeSetName,
				StackName:     in.StackName,
			})
			if err != nil {
				return nil, err
			}
			// Clean up and try making again
			res, err = cf.CreateChangeSetWithContext(ctx, in)
			if err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
	}
	a.cleanup.Add(func(ctx context.Context) error {
		_, err := cf.DeleteChangeSetWithContext(ctx, &cloudformation.DeleteChangeSetInput{
			ChangeSetName: res.Id,
		})
		return err
	})
	return a.waitForChangesetToFinishCreating(ctx, time.Second, cf, *res.Id, nil, nil)
}

func (a *AWSClients) ExecuteChangeset(ctx context.Context, changesetARN string) error {
	cf := cloudformation.New(a.session)
	_, err := cf.ExecuteChangeSetWithContext(ctx, &cloudformation.ExecuteChangeSetInput{
		ChangeSetName:      &changesetARN,
		ClientRequestToken: aws.String(a.token()),
	})
	return err
}

func (a *AWSClients) waitForChangesetToFinishCreating(ctx context.Context, pollInterval time.Duration, cloudformationClient *cloudformation.CloudFormation, changesetARN string, logger *logger.Logger, cleanShutdown <-chan struct{}) (*cloudformation.DescribeChangeSetOutput, error) {
	lastChangesetStatus := ""
	for {
		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
		case <-cleanShutdown:
			return nil, nil
		}
		out, err := cloudformationClient.DescribeChangeSetWithContext(ctx, &cloudformation.DescribeChangeSetInput{
			ChangeSetName: &changesetARN,
		})
		if err != nil {
			return nil, errors.Wrap(err, "unable to describe changeset")
		}
		stat := emptyOnNil(out.Status)
		if stat != lastChangesetStatus {
			logger.Log(1, "ChangeSet status set to %s: %s", stat, emptyOnNil(out.StatusReason))
			lastChangesetStatus = stat
		}
		// All terminal states
		if stat == "CREATE_COMPLETE" || stat == "FAILED" || stat == "DELETE_COMPLETE" {
			return out, nil
		}
	}
}

// waitForTerminalState loops forever until either the context ends, or something fails
func (a *AWSClients) WaitForTerminalState(ctx context.Context, stackID string, pollInterval time.Duration, log *logger.Logger) error {
	lastStackStatus := ""
	cfClient := cloudformation.New(a.session)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(pollInterval):
		}
		descOut, err := cfClient.DescribeStacksWithContext(ctx, &cloudformation.DescribeStacksInput{
			StackName: &stackID,
		})
		if err != nil {
			return err
		}
		if len(descOut.Stacks) != 1 {
			return errors.Errorf("unable to correctly find stack %s", stackID)
		}
		thisStack := descOut.Stacks[0]
		if *thisStack.StackStatus != lastStackStatus {
			log.Log(1, "Stack status set to %s: %s", *thisStack.StackStatus, emptyOnNil(thisStack.StackStatusReason))
			lastStackStatus = *thisStack.StackStatus
		}
		// https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-describing-stacks.html
		terminalFailureStatusStates := map[string]struct{}{
			"CREATE_FAILED":            {},
			"DELETE_FAILED":            {},
			"ROLLBACK_FAILED":          {},
			"ROLLBACK_COMPLETE":        {},
			"UPDATE_ROLLBACK_COMPLETE": {},
			"UPDATE_ROLLBACK_FAILED":   {},
		}
		if _, exists := terminalFailureStatusStates[emptyOnNil(thisStack.StackStatus)]; exists {
			return errors.Errorf("Terminal stack state failure: %s %s", emptyOnNil(thisStack.StackStatus), emptyOnNil(thisStack.StackStatusReason))
		}
		terminalOkStatusStates := map[string]struct{}{
			"CREATE_COMPLETE": {},
			"DELETE_COMPLETE": {},
			"UPDATE_COMPLETE": {},
		}
		if _, exists := terminalOkStatusStates[emptyOnNil(thisStack.StackStatus)]; exists {
			return nil
		}
	}
}

func emptyOnNil(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
