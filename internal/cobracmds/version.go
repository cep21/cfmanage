package cobracmds

import (
	"context"
	"html/template"
	"io"

	"github.com/cep21/cfmanage/internal/ctxfinder"
	"github.com/cep21/cfmanage/internal/logger"
	"github.com/google/go-github/v25/github"
	"github.com/spf13/cobra"
)

type versionCommand struct {
	Logger        *logger.Logger
	JSON          *bool
	ContextFinder *ctxfinder.ContextFinder
	GithubClient  *github.Client
}

func (s *versionCommand) Cobra() *cobra.Command {
	cmd := &cobra.Command{
		Use:       "version",
		Short:     "Display the current version (and latest version if different) of cfmanage",
		Example:   "cfexecute version",
		ValidArgs: []string{},
		Args:      cobra.NoArgs,
	}
	cmd.RunE = commonRunCommand(s.ContextFinder, s.model, s.JSON)
	return cmd
}

type versionCommandModel struct {
	CurrentVersion string
	LatestVersion  string
}

func (v *versionCommandModel) VersionMessage() string {
	if v.CurrentVersion != v.LatestVersion {
		return "Your version is out of date.  Download a newer one from https://github.com/cep21/ecsrun"
	}
	return ""
}

func tmpl() *template.Template {
	return template.Must(template.New("version").Parse(`{{ .VersionMessage }}
Current Version: {{ .CurrentVersion }}
Latest Version:  {{ .LatestVersion }}
`))
}

func (v *versionCommandModel) HumanReadable(out io.Writer) error {
	return tmpl().Execute(out, v)
}

const githubOwner = "cep21"
const githubRepo = "cfmanage"

func (s *versionCommand) model(ctx context.Context, cmd *cobra.Command, args []string) (HumanPrintable, error) {
	release, _, err := s.GithubClient.Repositories.GetLatestRelease(ctx, githubOwner, githubRepo)
	if err != nil {
		return nil, err
	}
	if release == nil || release.TagName == nil {
		return &versionCommandModel{
			CurrentVersion: currentVersion,
			LatestVersion:  "unknown",
		}, nil
	}
	latestRelease := *release.TagName
	if len(latestRelease) > 0 && latestRelease[0] == 'v' {
		latestRelease = latestRelease[1:]
	}

	return &versionCommandModel{
		CurrentVersion: currentVersion,
		LatestVersion:  latestRelease,
	}, nil
}
