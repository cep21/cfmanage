package cobracmds

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/cep21/cfexecute2/internal/awscache"
	"github.com/cep21/cfexecute2/internal/cleanup"
	"github.com/cep21/cfexecute2/internal/ctxfinder"
	"github.com/cep21/cfexecute2/internal/logger"
	"github.com/cep21/cfexecute2/internal/templatereader"
	"github.com/olekukonko/tablewriter"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"io"
	"time"
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
	cmd := &cobra.Command {
		Use:   "inspect [template] [params]",
		ValidArgs: s.T.ValidTemplatesAndParams(),
		Short: "Display status of all cloudformation stacks",
		Example: "cfexecute inspect infra canary",
		RunE: s.commandRun,
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 2 {
				return errors.New("expect exactly two arguments")
			}
			if err := validateTemplate(s.T, args[0]); err != nil {
				return err
			}
			if err := validateParams(s.T, args[0], args[1]); err != nil {
				return err
			}
			return nil
		},
	}
	return cmd
}

func validateTemplate(T *templatereader.TemplateFinder, t string) error {
	validTemplates, err := T.ValidateTemplate(t)
	if err == nil {
		return nil
	}
	fmt.Println("Valid templates:")
	for _, ts := range validTemplates {
		fmt.Println("  ", ts)
	}
	return err
}

func validateParams(T *templatereader.TemplateFinder, t string, p string) error {
	validParams, err := T.ValidateParameterFile(t, p)
	if err == nil {
		return nil
	}
	fmt.Printf("Valid Params for template %s:\n", t)
	for _, ts := range validParams {
		fmt.Println("  ", ts)
	}
	return err
}

func (s *inspectCommand) commandRun(cmd *cobra.Command, args []string) error {
	template := args[0]
	params := args[1]
	if err := validateTemplate(s.T, template); err != nil {
		return err
	}
	if err := validateParams(s.T, template, params); err != nil {
		return err
	}
	ctx := s.ContextFinder.Ctx()
	data, err := s.model(ctx, template, params)
	if err != nil {
		return errors.Wrap(err, "unable to load data for templates")
	}
	if *s.JSON == true {
		return json.NewEncoder(cmd.OutOrStdout()).Encode(data)
	}
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Stack summary\n"); err != nil {
		return err
	}
	printStatus(cmd.OutOrStdout(), []stackStatus{data.stackStatus})

	if err := printParams(cmd.OutOrStdout(), "Parameters", data.Parameters); err != nil {
		return err
	}

	if err := printParams(cmd.OutOrStdout(), "Outputs", data.Outputs); err != nil {
		return err
	}

	if err := printParams(cmd.OutOrStdout(), "Changes", data.Changes); err != nil {
		return err
	}
	return nil
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
	Parameters []param
	Outputs []param
	Changes []param
}

type param struct {
	Key string
	Value string
}

func (s *inspectCommand) model(ctx context.Context, template string, params string) (*inspectCommandModel, error) {
	stat, err := populateStatusCommand(ctx, s.Ctx, s.Logger, s.AWSCache, s.T, template, params)
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
				Key: emptyOnNil(o.OutputKey),
				Value: emptyOnNil(o.OutputValue),
			})
		}
	}
	if stat.changeset != nil {
		ret.Parameters = make([]param, 0, len(stat.changeset.Parameters))
		for _, p := range stat.changeset.Parameters {
			ret.Parameters = append(ret.Parameters, param{
				Key: emptyOnNil(p.ParameterKey),
				Value: firstNonEmpty(emptyOnNil(p.ResolvedValue), emptyOnNil(p.ParameterValue)),
			})
		}
		ret.Changes = make([]param, 0, len(stat.changeset.Changes))
		for _, c := range stat.changeset.Changes {
			ret.Changes = append(ret.Changes, param{
				Key: firstNonEmpty(emptyOnNil(c.ResourceChange.PhysicalResourceId), emptyOnNil(c.ResourceChange.LogicalResourceId)),
				Value: emptyOnNil(c.ResourceChange.Action),
			})
		}
	}
	return ret, nil
}

func firstNonEmpty(s...string) string {
	for _, ret := range s {
		if ret != "" {
			return ret
		}
	}
	return ""
}

func emptyOnNil(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
