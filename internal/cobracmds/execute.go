package cobracmds

import (
	"context"
	"fmt"
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
	"os"
	"time"
)

type executeCommand struct {
	AWSCache      *awscache.AWSCache
	T             *templatereader.TemplateFinder
	Ctx           *templatereader.CreateChangeSetTemplate
	Logger        *logger.Logger
	JSON          *bool
	ContextFinder *ctxfinder.ContextFinder
	Cleanup       *cleanup.Cleanup
	autoConfirm   bool
}

func (s *executeCommand) Cobra() *cobra.Command {
	cmd := &cobra.Command {
		Use:   "execute [template] [params]",
		ValidArgs: s.T.ValidTemplatesAndParams(),
		Short: "Execute a cloudformation update for a stack",
		Example: "cfexecute execute infra canary",
		RunE: s.commandRun,
	}
	cmd.Flags().BoolVarP(&s.autoConfirm, "auto", "a", false, "Will auto confirm the cloudformation change")
	cmd.Args = validateTemplateParam(s.T)
	return cmd
}

func (s *executeCommand) commandRun(cmd *cobra.Command, args []string) error {
	template := args[0]
	params := args[1]
	if err := validateTemplate(s.T, template); err != nil {
		return err
	}
	if err := validateParams(s.T, template, params); err != nil {
		return err
	}
	ctx := s.ContextFinder.Ctx()
	data, err := s.modelPhase1(ctx, template, params)
	if err != nil {
		return errors.Wrap(err, "unable to load data for templates")
	}
	if data.StackStatus != "CREATE_COMPLETE" && data.StackStatus != "UPDATE_COMPLETE" {
		return fmt.Errorf("unable to create stack.  Status: %s", data.StackStatus)
	}
	if err := display(cmd.OutOrStdout(), s.JSON, data); err != nil {
		return err
	}
	if len(data.Changes) == 0 {
		return display(cmd.OutOrStdout(), s.JSON, printableString("no changes\n"))
	}
	if !s.autoConfirm {
		if !confirm(os.Stdin, cmd.OutOrStdout(), "Execute this cloudformation", 3, nil) {
			return nil
		}
	}


	return s.modelPhase2(ctx, cmd.OutOrStdout(), data)
}

func (s *executeCommand) modelPhase1(ctx context.Context, template string, params string) (*inspectCommandModel, error) {
	stats, err := populateInspectCommand(ctx, s.Ctx, s.Logger, s.AWSCache, s.T, template, params)
	if err != nil {
		return nil, err
	}
	return stats, nil
}

type stackEvent struct {
	LogicalResourceID    string `json:",omitempty"`
	PhysicalResourceID   string `json:",omitempty"`
	ResourceStatus       string `json:",omitempty"`
	ResourceStatusReason string `json:",omitempty"`
	ResourceType         string `json:",omitempty"`
}

type printableString string

func (p printableString) HumanReadable(out io.Writer) error {
	_, err := io.WriteString(out, (string)(p))
	return err
}

func (s *stackEvent) HumanReadable(out io.Writer) error {
	table4 := tablewriter.NewWriter(out)
	table4.SetHeader([]string{"LogicalResourceID", "PhysicalResourceID", "ResourceStatus", "ResourceStatusReason", "ResourceType"})
	table4.Append([]string{s.LogicalResourceID, s.PhysicalResourceID, s.ResourceStatus, s.ResourceStatusReason, s.ResourceType})
	table4.Render()
	return nil
}

func (s *executeCommand) printStackEvents(ctx context.Context, out io.Writer, in chan *cloudformation.StackEvent) error {
	for {
		select {
		case <- ctx.Done():
			return ctx.Err()
		case event, ok := <- in:
			if !ok {
				return nil
			}
			p := &stackEvent {
				LogicalResourceID: emptyOnNil(event.LogicalResourceId),
				PhysicalResourceID: emptyOnNil(event.PhysicalResourceId),
				ResourceStatus: emptyOnNil(event.ResourceStatus),
				ResourceStatusReason: emptyOnNil(event.ResourceStatusReason),
				ResourceType: emptyOnNil(event.ResourceType),
			}
			if err := display(out, s.JSON, p); err != nil {
				return err
			}
		}
	}
}

var errFinishedOk = errors.New("finished ok")

func (s *executeCommand) modelPhase2(ctx context.Context, out io.Writer, inspectModel *inspectCommandModel) error {
	ses, err := s.AWSCache.Session(inspectModel.changesetInput.Profile, inspectModel.changesetInput.Region)
	if err != nil {
		return err
	}
	err = ses.ExecuteChangeset(ctx, *inspectModel.changeset.ChangeSetId)
	if err != nil {
		return errors.Wrapf(err, "unable to execute changeset %s", *inspectModel.changeset.ChangeSetId)
	}

	// Now stream the changes
	streamer := awscache.StackStreamer{
	}
	eg, egCtx := errgroup.WithContext(ctx)
	streamInto := make(chan *cloudformation.StackEvent)
	eg.Go(func() error {
		defer close(streamInto)
		return streamer.Start(egCtx, ses, *inspectModel.changeset.StackId, streamInto)
	})
	eg.Go(func() error {
		return s.printStackEvents(egCtx, out, streamInto)
	})
	eg.Go(func() error {
		actualErr := ses.WaitForTerminalState(egCtx, *inspectModel.changeset.StackId, time.Second, s.Logger)
		if actualErr == nil {
			return errFinishedOk
		}
		return actualErr
	})
	err = eg.Wait()
	if err == errFinishedOk {
		return nil
	}
	return err
}