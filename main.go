package main

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/cep21/cfmanage/internal/awscache"
	"github.com/cep21/cfmanage/internal/cleanup"
	"github.com/cep21/cfmanage/internal/cobracmds"
	"github.com/cep21/cfmanage/internal/ctxfinder"
	"github.com/cep21/cfmanage/internal/logger"
	"github.com/cep21/cfmanage/internal/templatereader"
)

var App = Application{ //nolint
	Out: os.Stdout,
}

func main() {
	App.main()
}

type Application struct {
	Out io.Writer
}

func (a *Application) main() {
	l := &logger.Logger{
		Logger: log.New(a.Out, "cfmanage", log.LstdFlags),
	}
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
