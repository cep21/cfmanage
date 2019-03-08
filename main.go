package main

import (
	"fmt"
	"github.com/cep21/cfexecute2/internal/awscache"
	"github.com/cep21/cfexecute2/internal/cleanup"
	"github.com/cep21/cfexecute2/internal/cobracmds"
	"github.com/cep21/cfexecute2/internal/ctxfinder"
	"github.com/cep21/cfexecute2/internal/logger"
	"github.com/cep21/cfexecute2/internal/templatereader"
	"io"
	"os"
)

var App = Application{
	Out: os.Stdout,
}

func main() {
	App.main()
}

type Application struct {
	Out io.Writer
}

func (a *Application) main() {
	l := &logger.Logger{}
	Cleanup := &cleanup.Cleanup{}
	rootCmd := cobracmds.RootCommand{
		AWSCache: &awscache.AWSCache{
			Cleanup: Cleanup,
		},
		T: &templatereader.TemplateFinder{
			Logger: l,
		},
		Ctx:           &templatereader.CreateChangeSetTemplate{},
		Logger:        l,
		Out:           a.Out,
		Cleanup:       Cleanup,
		ContextFinder: &ctxfinder.ContextFinder{},
	}

	if err := rootCmd.Cobra().Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
