package awscache

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"

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
		return nil, errors.Wrapf(err, "unable to make session for profile %s", profile)
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

func isAlreadyExistsException(err error) bool {
	return isAWSError(err, "AlreadyExistsException")
}

func isAWSError(err error, code string) bool {
	if err == nil {
		return false
	}
	r := errors.Cause(err)
	if ae, ok := r.(awserr.Error); ok {
		return ae.Code() == code
	}
	return strings.Contains(r.Error(), code)
}

func (a *AWSClients) createChangeset(ctx context.Context, cf *cloudformation.CloudFormation, in *cloudformation.CreateChangeSetInput, hasAlreadyDeletedChangeSet bool) (*cloudformation.CreateChangeSetOutput, error) {
	res, err := cf.CreateChangeSetWithContext(ctx, in)
	if err == nil {
		return res, nil
	}
	if !hasAlreadyDeletedChangeSet && isAlreadyExistsException(err) {
		_, err := cf.DeleteChangeSetWithContext(ctx, &cloudformation.DeleteChangeSetInput{
			ChangeSetName: in.ChangeSetName,
			StackName:     in.StackName,
		})
		if err != nil {
			return nil, errors.Wrap(err, "deleting changeset failed")
		}
		return a.createChangeset(ctx, cf, in, true)
	}
	return nil, errors.Wrap(err, "unable to create changeset")
}

func stringsReplaceAllRepeated(s string, old string, new string) string {
	prev := len(s)
	for len(s) > 0 {
		s = strings.Replace(s, old, new, -1)
		if prev == len(s) {
			return s
		}
	}
	return s
}

func sanitizeBucketName(s string) string {
	// from https://docs.aws.amazon.com/awscloudtrail/latest/userguide/cloudtrail-s3-bucket-naming-requirements.html
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '.' || r == '-':
			return r
		}
		return '-'
	}, s)
	if len(s) < 3 {
		s = "aaa"
	}
	if s[0] == '-' || s[0] == '.' {
		s = "a" + s
	}
	s = strings.TrimSuffix(s, "-")
	s = stringsReplaceAllRepeated(s, "..", ".")
	s = stringsReplaceAllRepeated(s, ".-", "-")
	s = stringsReplaceAllRepeated(s, "-.", "-")
	return s
}

func (a *AWSClients) FixTemplateBody(ctx context.Context, in *cloudformation.CreateChangeSetInput, bucket string, logger *logger.Logger) error {
	if in.TemplateBody == nil {
		return nil
	}
	tb := *in.TemplateBody
	// Actual number is 51200 but we give ourselves some buffer
	if len(tb) < 51100 {
		return nil
	}
	logger.Log(1, "template body too large (%d): setting in s3", len(tb))
	if bucket == "" {
		bucket = sanitizeBucketName(fmt.Sprintf("cfmanage_%s", *in.StackName))
		logger.Log(1, "Making bucket %s because no bucket set", bucket)
		clients3 := s3.New(a.session)
		out, err := clients3.CreateBucket(&s3.CreateBucketInput{
			Bucket: &bucket,
		})
		if err != nil {
			if !isAWSError(err, "BucketAlreadyOwnedByYou") {
				return errors.Wrapf(err, "unable to create bucket %s correctly", bucket)
			}
			logger.Log(1, "bucket already owend by you")
		} else {
			logger.Log(1, "Bucket created with URL %s", *out.Location)
		}
	}
	uploader := s3manager.NewUploader(a.session)
	itemKey := fmt.Sprintf("cfmanage_%s_%s", *in.StackName, time.Now().UTC())
	out, err := uploader.UploadWithContext(ctx, &s3manager.UploadInput{
		Bucket: &bucket,
		Key:    &itemKey,
		Body:   strings.NewReader(tb),
	})
	if err != nil {
		return errors.Wrapf(err, "unable to upload body to bucket %s", bucket)
	}
	logger.Log(1, "template body uploaded to %s", out.Location)
	in.TemplateBody = nil
	in.TemplateURL = &out.Location
	a.cleanup.Add(func(ctx context.Context) error {
		logger.Log(2, "Cleaning up %s/%s", bucket, itemKey)
		clients3 := s3.New(a.session)
		_, err := clients3.DeleteObject(&s3.DeleteObjectInput{
			Bucket: &bucket,
			Key:    &itemKey,
		})
		return errors.Wrapf(err, "Unable to delete bucket=%s key=%s", bucket, itemKey)

	})
	return nil
}

func (a *AWSClients) CreateChangesetWaitForStatus(ctx context.Context, in *cloudformation.CreateChangeSetInput, existingStack *cloudformation.Stack, logger *logger.Logger) (*cloudformation.DescribeChangeSetOutput, error) {
	if in.ChangeSetName == nil {
		in.ChangeSetName = aws.String("A" + strconv.FormatInt(time.Now().UnixNano(), 16))
	}
	in.ClientToken = aws.String(a.token())
	cf := cloudformation.New(a.session)
	in = guessChangesetType(ctx, cf, in)

	res, err := a.createChangeset(ctx, cf, in, false)
	if err != nil {
		return nil, errors.Wrap(err, "creating changeset failed")
	}
	a.cleanup.Add(func(ctx context.Context) error {
		_, err := cf.DeleteChangeSetWithContext(ctx, &cloudformation.DeleteChangeSetInput{
			ChangeSetName: res.Id,
		})
		return err
	})
	if existingStack == nil {
		// Clean up the stack created by the changeset
		a.cleanup.Add(func(ctx context.Context) error {
			finishingStack, err := a.DescribeStack(ctx, *in.StackName)
			if err != nil {
				return errors.Wrapf(err, "unable to describe stack %s", *in.StackName)
			}
			if *finishingStack.StackStatus == "REVIEW_IN_PROGRESS" {
				_, err := cf.DeleteStack(&cloudformation.DeleteStackInput{
					ClientRequestToken: aws.String(a.token()),
					StackName:          in.StackName,
				})
				return errors.Wrapf(err, "unable to delete stack %s", *in.StackName)
			}
			return nil
		})
	}
	return a.waitForChangesetToFinishCreating(ctx, time.Second, cf, *res.Id, logger, nil)
}

func (a *AWSClients) ExecuteChangeset(ctx context.Context, changesetARN string) error {
	cf := cloudformation.New(a.session)
	_, err := cf.ExecuteChangeSetWithContext(ctx, &cloudformation.ExecuteChangeSetInput{
		ChangeSetName:      &changesetARN,
		ClientRequestToken: aws.String(a.token()),
	})
	return errors.Wrapf(err, "unable to execute changeset %s", changesetARN)
}

func (a *AWSClients) CancelStackUpdate(ctx context.Context, stackName string) error {
	cf := cloudformation.New(a.session)
	_, err := cf.CancelUpdateStackWithContext(ctx, &cloudformation.CancelUpdateStackInput{
		// Note: Stack cancels should *not* use the same client request token as the create request
		StackName: &stackName,
	})
	return errors.Wrapf(err, "unable to cancel stack update to %s", stackName)
}

func (a *AWSClients) waitForChangesetToFinishCreating(ctx context.Context, pollInterval time.Duration, cloudformationClient *cloudformation.CloudFormation, changesetARN string, logger *logger.Logger, cleanShutdown <-chan struct{}) (*cloudformation.DescribeChangeSetOutput, error) {
	lastChangesetStatus := ""
	for {
		select {
		case <-time.After(pollInterval):
		case <-ctx.Done():
			return nil, errors.Wrapf(ctx.Err(), "context died waiting for changeset %s", changesetARN)
		case <-cleanShutdown:
			return nil, nil
		}
		out, err := cloudformationClient.DescribeChangeSetWithContext(ctx, &cloudformation.DescribeChangeSetInput{
			ChangeSetName: &changesetARN,
		})
		if err != nil {
			return nil, errors.Wrapf(err, "unable to describe changeset %s", changesetARN)
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
			return errors.Wrap(ctx.Err(), "context died waiting for terminal state")
		case <-time.After(pollInterval):
		}
		descOut, err := cfClient.DescribeStacksWithContext(ctx, &cloudformation.DescribeStacksInput{
			StackName: &stackID,
		})
		if err != nil {
			return errors.Wrapf(err, "unable to describe stack %s", stackID)
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
			"UPDATE_ROLLBACK_FAILED":   {},
			"ROLLBACK_COMPLETE":        {},
			"UPDATE_ROLLBACK_COMPLETE": {},
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
