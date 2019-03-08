package cobracmds

import (
	"context"
	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/cep21/cfexecute2/internal/awscache"
	"github.com/cep21/cfexecute2/internal/cleanup"
	"github.com/cep21/cfexecute2/internal/ctxfinder"
	"github.com/cep21/cfexecute2/internal/logger"
	"github.com/cep21/cfexecute2/internal/templatereader"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
	"io"
	"strconv"
)

type statusCommand struct {
	AWSCache      *awscache.AWSCache
	T             *templatereader.TemplateFinder
	Ctx           *templatereader.CreateChangeSetTemplate
	Logger        *logger.Logger
	JSON          *bool
	ContextFinder *ctxfinder.ContextFinder
	Cleanup       *cleanup.Cleanup
}

func (s *statusCommand) Cobra() *cobra.Command {
	cmd := &cobra.Command {
		Use:   "status",
		Short: "Display status of all cloudformation stacks",
		Example: "cfexecute status",
		ValidArgs: []string{},
		Args: cobra.NoArgs,
	}
	cmd.RunE = commonRunCommand(s.ContextFinder, s.model, s.JSON)
	return cmd
}

func printStatus(out io.Writer, statuses[]stackStatus) {
	table := tablewriter.NewWriter(out)
	table.SetHeader([]string{"Template", "File name", "Stack Name", "Status", "Account ID", "Region", "Pending Changes", "Description", "Last Updated"})
	for _, st := range statuses {
		table.Append([]string{
			st.Template, st.StackFileName, st.StackName, st.StackStatus, st.AccountID, st.Region, st.ChangeCount, st.Description, st.LastUpdated,
		})
	}
	table.Render()
}

type statusCommandModel struct {
	Statuses []stackStatus
}

func (s *statusCommandModel) HumanReadable(out io.Writer) error {
	table := tablewriter.NewWriter(out)
	table.SetHeader([]string{"Template", "File name", "Stack Name", "Status", "Account ID", "Region", "Pending Changes", "Description", "Last Updated"})
	for _, st := range s.Statuses {
		table.Append([]string{
			st.Template, st.StackFileName, st.StackName, st.StackStatus, st.AccountID, st.Region, st.ChangeCount, st.Description, st.LastUpdated,
		})
	}
	table.Render()
	return nil
}

type stackStatus struct {
	Template      string
	StackFileName string
	StackName     string
	StackStatus   string
	AccountID     string
	Region        string
	ChangeCount   string
	Description string
	LastUpdated string

	cfStack *cloudformation.Stack
	changeset *cloudformation.DescribeChangeSetOutput
	changesetInput *templatereader.ChangesetInput
}

func (s stackStatus) goodStatus() bool {
	if s.StackStatus == "CREATE_COMPLETE" || s.StackStatus == "UPDATE_COMPLETE" {
		return true
	}
	return false
}

func populateStatusCommand(ctx context.Context, createTemplate *templatereader.CreateChangeSetTemplate, log *logger.Logger, awsCache *awscache.AWSCache, T *templatereader.TemplateFinder, t string, p string) (stackStatus, error) {
	log.Log(2, "Listing params %s", p)
	fname := T.ParameterFilename(t, p)
	in, err := templatereader.LoadCreateChangeSet(fname, createTemplate, log)
	if err != nil {
		return stackStatus{
			Template: t,
			StackFileName: fname,
			StackStatus: err.Error(),
		}, nil
	}
	ses, err := awsCache.Session(in.Profile, in.Region)
	if err != nil {
		return stackStatus{}, errors.Wrapf(err, "unable to fetch AWS session for profile %s", in.Profile)
	}
	statStatus, err := ses.DescribeStack(ctx, *in.StackName)
	if err != nil {
		return stackStatus{
			Template: t,
			StackFileName: fname,
			StackName: *in.StackName,
			StackStatus: err.Error(),
			AccountID: readable(ses.AccountID()),
			Region: ses.Region(),
			cfStack: statStatus,
			changesetInput: in,
		}, nil
	}
	if statStatus == nil {
		return stackStatus{
			Template: t,
			StackFileName: fname,
			StackName: p,
			StackStatus: "--DOES NOT EXIST--",
			AccountID: readable(ses.AccountID()),
			Region: ses.Region(),
			cfStack: statStatus,
			changesetInput: in,
		}, nil
	} else {
		out, err := ses.CreateChangesetWaitForStatus(ctx, &in.CreateChangeSetInput)
		if err != nil {
			return stackStatus{}, err
		}
		return stackStatus{
			Description: emptyOnNil(statStatus.Description),
			LastUpdated: emptyOnNilTime(statStatus.LastUpdatedTime),
			Template: t,
			StackFileName: fname,
			StackName: *in.StackName,
			StackStatus: *statStatus.StackStatus,
			AccountID: readable(ses.AccountID()),
			Region: ses.Region(),
			ChangeCount: strconv.Itoa(len(out.Changes)),
			cfStack: statStatus,
			changeset: out,
			changesetInput: in,
		}, nil
	}
}

func (s *statusCommand) model(ctx context.Context, cmd *cobra.Command, args []string) (HumanPrintable, error) {
	s.Logger.Log(2, "Running status command")
	templates, err := s.T.ListTemplates()
	if err != nil {
		return nil, errors.Wrap(err, "unable to list all templates")
	}
	statuses := make([][]stackStatus, len(templates))
	ret := statusCommandModel{
	}
	eg, egCtx := errgroup.WithContext(ctx)
	for tidx, t := range templates {
		s.Logger.Log(2, "Listing template %s", t)
		params, err := s.T.ListParameters(t)
		if err != nil {
			return nil, errors.Wrapf(err, "uanble to list parameters for template %s", t)
		}
		statuses[tidx] = make([]stackStatus, len(params))
		for idx, p := range params {
			p := p
			t := t
			idx := idx
			tidx := tidx
			eg.Go(func() error {
				stat, err := populateStatusCommand(egCtx, s.Ctx, s.Logger, s.AWSCache, s.T, t, p)
				if err != nil {
					return err
				}
				statuses[tidx][idx] = stat
				return nil
			})
		}
	}
	if err := eg.Wait(); err != nil {
		return nil, err
	}
	for _, st := range statuses {
		for _, st2 := range st {
			ret.Statuses = append(ret.Statuses, st2)
		}
	}
	return &ret, nil
}
