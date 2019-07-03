package templatereader

import (
	"bytes"
	"encoding/json"
	"io"
	"io/ioutil"
	"os"
	"text/template"

	"github.com/aws/aws-sdk-go/service/cloudformation"
	"github.com/cep21/cfmanage/internal/logger"
	"github.com/pkg/errors"
)

type ChangesetInput struct {
	cloudformation.CreateChangeSetInput
	Profile string `json:"profile"`
	Region  string `json:"region"`
	Bucket  string `json:"bucket"`
}

func LoadCreateChangeSet(changesetFilename string, translator *CreateChangeSetTemplate, logger *logger.Logger) (*ChangesetInput, error) {
	f, err := os.Open(changesetFilename)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to open file %s (does it exist?)", changesetFilename)
	}
	return translator.createRegisterTaskDefinitionInput(f, logger)
}

// CreateChangeSetTemplate is passed to the changeset.json file when Executing the template
type CreateChangeSetTemplate struct {
	Ctx
}

func (t *CreateChangeSetTemplate) createRegisterTaskDefinitionInput(in io.Reader, logger *logger.Logger) (*ChangesetInput, error) {
	readerContents, err := ioutil.ReadAll(in)
	if err != nil {
		return nil, errors.Wrap(err, "unable to fully read from reader (verify your reader)")
	}
	taskTemplate, err := template.New("task_template").Parse(string(readerContents))
	if err != nil {
		return nil, errors.Wrap(err, "invalid task template (make sure your task template is ok)")
	}
	logger.Log(2, "Task template: %s", string(readerContents))
	var templateResult bytes.Buffer
	if err := taskTemplate.Execute(&templateResult, t); err != nil {
		return nil, errors.Wrap(err, "unable to execute task template (are you calling invalid functions?)")
	}
	logger.Log(2, "Executed template result: %s", templateResult.String())
	var out ChangesetInput
	if err := json.NewDecoder(&templateResult).Decode(&out); err != nil {
		templateResult.Reset()
		logger.Log(1, "Failing template body: %s", templateResult.String())
		return nil, errors.Wrap(err, "unable to deserialize given template (is it valid json?)")
	}
	return &out, nil
}
