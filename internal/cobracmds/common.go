package cobracmds

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/cep21/cfexecute2/internal/ctxfinder"
	"github.com/cep21/cfexecute2/internal/templatereader"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"io"
	"strings"
	"time"
)

func commonRunCommand(f *ctxfinder.ContextFinder, generateModel func(ctx context.Context, cmd *cobra.Command, args []string) (HumanPrintable, error), useJSON *bool) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		ctx := f.Ctx()
		data, err := generateModel(ctx, cmd, args)
		if err != nil {
			return errors.Wrap(err, "unable to load data for templates")
		}
		return display(cmd.OutOrStdout(), useJSON, data)
	}
}

func display(out io.Writer, useJSON *bool, data HumanPrintable) error {
	if *useJSON == true {
		return json.NewEncoder(out).Encode(data)
	}
	return data.HumanReadable(out)
}

type HumanPrintable interface {
	HumanReadable(out io.Writer) error
}

func emptyOnNilTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.String()
}

func readable(s string, err error) string {
	if err != nil {
		return err.Error()[:10]
	}
	return s
}

func firstNonEmpty(s ...string) string {
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

func validateTemplateParam(T *templatereader.TemplateFinder) func(*cobra.Command, []string) error {
	return func(_ *cobra.Command, args []string) error {
		if len(args) != 2 {
			return errors.New("expect exactly two arguments")
		}
		if err := validateTemplate(T, args[0]); err != nil {
			return err
		}
		if err := validateParams(T, args[0], args[1]); err != nil {
			return err
		}
		return nil
	}
}

// confirm displays a prompt `s` to the user and returns a bool indicating yes / no
// If the lowercased, trimmed input begins with anything other than 'y', it returns false
// It accepts an int `tries` representing the number of attempts before returning false
func confirm(in io.Reader, out io.Writer, prompt string, tries int, _ <-chan struct{}) bool { //nolint: unparam
	r := bufio.NewReader(in)

	for ; tries > 0; tries-- {
		if _, err := fmt.Fprintf(out, "%s [y/n]: ", prompt); err != nil {
			return false
		}

		res, err := r.ReadString('\n')
		if err != nil {
			return false
		}

		// Empty input (i.e. "\n")
		if len(res) < 2 {
			continue
		}

		return strings.ToLower(strings.TrimSpace(res))[0] == 'y'
	}

	return false
}
