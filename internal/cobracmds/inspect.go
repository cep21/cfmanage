package cobracmds

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/cep21/cfmanage/internal/awscache"
	"github.com/cep21/cfmanage/internal/cleanup"
	"github.com/cep21/cfmanage/internal/ctxfinder"
	"github.com/cep21/cfmanage/internal/logger"
	"github.com/cep21/cfmanage/internal/templatereader"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
)

type inspectCommand struct {
	AWSCache      *awscache.AWSCache
	T             *templatereader.TemplateFinder
	Ctx           *templatereader.CreateChangeSetTemplate
	Logger        *logger.Logger
	JSON          *bool
	ContextFinder *ctxfinder.ContextFinder
	Cleanup       *cleanup.Cleanup
}

func (s *inspectCommand) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "inspect [template] [params]",
		ValidArgs: s.T.ValidTemplatesAndParams(),
		Short:     "Display status of all cloudformation stacks",
		Example:   "cfexecute inspect infra canary",
	}
	cmd.Args = validateTemplateParam(s.T)
	cmd.RunE = commonRunCommand(s.ContextFinder, s.model, s.JSON)
	return cmd
}

func validateTemplate(tfinder *templatereader.TemplateFinder, t string) error {
	validTemplates, err := tfinder.ValidateTemplate(t)
	if err == nil {
		return nil
	}
	fmt.Println("Valid templates:")
	for _, ts := range validTemplates {
		fmt.Println("  ", ts)
	}
	return err
}

func validateParams(tfinder *templatereader.TemplateFinder, t string, p string) error {
	validParams, err := tfinder.ValidateParameterFile(t, p)
	if err == nil {
		return nil
	}
	fmt.Printf("Valid Params for template %s:\n", t)
	for _, ts := range validParams {
		fmt.Println("  ", ts)
	}
	return err
}

func printParams(out io.Writer, title string, params []param) error {
	if _, err := fmt.Fprintf(out, "%s\n", title); err != nil {
		return err
	}
	if len(params) == 0 {
		_, err := fmt.Fprintf(out, "<NONE>\n")
		return err
	}
	table4 := tablewriter.NewWriter(out)
	table4.SetHeader([]string{"Key", "Value"})
	for _, p := range params {
		table4.Append([]string{
			p.Key, p.Value,
		})
	}
	table4.Render()
	return nil
}

type inspectCommandModel struct {
	stackStatus
	Description string
	LastUpdated time.Time
	Parameters  []param
	Outputs     []param
	Changes     []param
}

func (i *inspectCommandModel) HumanReadable(out io.Writer) error {
	if _, err := fmt.Fprintf(out, "Stack summary\n"); err != nil {
		return err
	}
	printStatus(out, []stackStatus{i.stackStatus})

	if err := printParams(out, "Parameters", i.Parameters); err != nil {
		return err
	}

	if err := printParams(out, "Outputs", i.Outputs); err != nil {
		return err
	}

	if err := printParams(out, "Changes", i.Changes); err != nil {
		return err
	}
	return nil
}

type param struct {
	Key   string
	Value string
}

func (s *inspectCommand) model(ctx context.Context, cmd *cobra.Command, args []string) (HumanPrintable, error) {
	template := args[0]
	params := args[1]
	return populateInspectCommand(ctx, s.Ctx, s.Logger, s.AWSCache, s.T, template, params)
}

func populateInspectCommand(ctx context.Context, createTemplate *templatereader.CreateChangeSetTemplate, log *logger.Logger, awsCache *awscache.AWSCache, tfinder *templatereader.TemplateFinder, template string, params string) (*inspectCommandModel, error) {
	stat, err := populateStatusCommand(ctx, createTemplate, log, awsCache, tfinder, template, params)
	if err != nil {
		return nil, err
	}
	ret := &inspectCommandModel{
		stackStatus: stat,
	}
	if stat.cfStack != nil {
		if stat.cfStack.LastUpdatedTime != nil {
			ret.LastUpdated = *stat.cfStack.LastUpdatedTime
		} else if stat.cfStack.CreationTime != nil {
			ret.LastUpdated = *stat.cfStack.CreationTime
		}
		ret.Description = emptyOnNil(stat.cfStack.Description)
		for _, o := range stat.cfStack.Outputs {
			ret.Outputs = append(ret.Outputs, param{
				Key:   emptyOnNil(o.OutputKey),
				Value: emptyOnNil(o.OutputValue),
			})
		}
	}
	if stat.changeset != nil {
		ret.Parameters = make([]param, 0, len(stat.changeset.Parameters))
		for _, p := range stat.changeset.Parameters {
			ret.Parameters = append(ret.Parameters, param{
				Key:   emptyOnNil(p.ParameterKey),
				Value: firstNonEmpty(emptyOnNil(p.ResolvedValue), emptyOnNil(p.ParameterValue)),
			})
		}
		ret.Changes = make([]param, 0, len(stat.changeset.Changes))
		for _, c := range stat.changeset.Changes {
			ret.Changes = append(ret.Changes, param{
				Key:   firstNonEmpty(emptyOnNil(c.ResourceChange.PhysicalResourceId), emptyOnNil(c.ResourceChange.LogicalResourceId)),
				Value: emptyOnNil(c.ResourceChange.Action),
			})
		}
	}
	return ret, nil
}
