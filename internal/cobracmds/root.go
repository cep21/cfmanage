package cobracmds

import (
	"io"
	"time"

	"github.com/google/go-github/v25/github"

	"github.com/cep21/cfmanage/internal/awscache"
	"github.com/cep21/cfmanage/internal/cleanup"
	"github.com/cep21/cfmanage/internal/ctxfinder"
	"github.com/cep21/cfmanage/internal/logger"
	"github.com/cep21/cfmanage/internal/templatereader"
	"github.com/spf13/cobra"
)

type RootCommand struct {
	AWSCache      *awscache.AWSCache
	T             *templatereader.TemplateFinder
	Ctx           *templatereader.CreateChangeSetTemplate
	Logger        *logger.Logger
	Out           io.Writer
	JSONFormat    bool
	Cleanup       *cleanup.Cleanup
	ContextFinder *ctxfinder.ContextFinder
}

const currentVersion = "1.2.0"

func (s *RootCommand) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cfmanage",
		Short:   "Execute and manage a set of cloudformation files",
		Long:    "cfmanage lets you manage a wide set of cloudformation files that represent many stacks at once",
		Example: "cfexecute",
		Version: currentVersion,
		PersistentPostRun: func(cmd *cobra.Command, args []string) {
			s.Cleanup.Clean()
		},
	}
	cmd.PersistentFlags().IntVarP(&s.Logger.Verbosity, "verbosity", "v", 0, "Output verbosity.  Higher is more verbose")
	cmd.PersistentFlags().DurationVarP(&s.ContextFinder.Timeout, "timeout", "t", 0, "If non zero, will time out commands on this value")
	cmd.PersistentFlags().DurationVar(&s.Cleanup.CleanupTimeout, "cleantimeout", time.Second, "How long to wait for cleanup jobs to finish (in addition to the timeout of the script itself)")
	cmd.PersistentFlags().StringVarP(&s.T.BaseDir, "dir", "d", "cloudformation", "Directory containing cloudformation files")
	cmd.PersistentFlags().BoolVarP(&s.JSONFormat, "json", "j", false, "If true, will output as JSON")
	if s.Out != nil {
		cmd.SetOutput(s.Out)
	}

	statusCmd := &statusCommand{
		AWSCache:      s.AWSCache,
		T:             s.T,
		Ctx:           s.Ctx,
		Logger:        s.Logger,
		JSON:          &s.JSONFormat,
		ContextFinder: s.ContextFinder,
		Cleanup:       s.Cleanup,
	}
	cmd.AddCommand(statusCmd.Cobra())

	inspectCmd := &inspectCommand{
		AWSCache:      s.AWSCache,
		T:             s.T,
		Ctx:           s.Ctx,
		Logger:        s.Logger,
		JSON:          &s.JSONFormat,
		ContextFinder: s.ContextFinder,
		Cleanup:       s.Cleanup,
	}
	cmd.AddCommand(inspectCmd.Cobra())

	executeCommand := &executeCommand{
		AWSCache:      s.AWSCache,
		T:             s.T,
		Ctx:           s.Ctx,
		Logger:        s.Logger,
		JSON:          &s.JSONFormat,
		ContextFinder: s.ContextFinder,
		Cleanup:       s.Cleanup,
	}
	cmd.AddCommand(executeCommand.Cobra())

	versionCommand := &versionCommand{
		Logger:        s.Logger,
		JSON:          &s.JSONFormat,
		ContextFinder: s.ContextFinder,
		GithubClient:  github.NewClient(nil),
	}
	cmd.AddCommand(versionCommand.Cobra())
	return cmd
}
