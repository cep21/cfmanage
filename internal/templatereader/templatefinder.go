package templatereader

import (
	"fmt"
	"github.com/cep21/cfexecute2/internal/logger"
	"io/ioutil"
	"path"
	"strings"
)

type TemplateFinder struct {
	BaseDir string
	Logger *logger.Logger
}

func (t *TemplateFinder) ValidateTemplate(tmpl string) ([]string, error) {
	validTemplates, err := t.ListTemplates()
	if err != nil {
		return nil, err
	}
	for _, ts := range validTemplates {
		if ts == tmpl {
			return validTemplates, nil
		}
	}
	return validTemplates, fmt.Errorf("invalid template: %s", tmpl)
}

func (t *TemplateFinder) ValidateParameterFile(tmpl string, param string) ([]string, error) {
	validParams, err := t.ListParameters(tmpl)
	if err != nil {
		return nil, err
	}
	for _, ps := range validParams {
		if ps == param {
			return validParams, nil
		}
	}
	return validParams, fmt.Errorf("invalid parameter file: %s", param)
}

func (t *TemplateFinder) ValidTemplatesAndParams() []string {
	var ret []string
	tmps, err := t.ListTemplates()
	if err != nil {
		return nil
	}
	ret = append(ret, tmps...)
	for _, tp := range tmps {
		params, err := t.ListParameters(tp)
		if err != nil {
			return nil
		}
		ret = append(ret, params...)
	}
	return ret
}

func (t *TemplateFinder) ListTemplates() ([]string, error) {
	fi, err := ioutil.ReadDir(t.BaseDir)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(fi))
	for _, f := range fi {
		if f.IsDir() {
			names = append(names, f.Name())
		}
	}
	return names, nil
}

func (t *TemplateFinder) ListParameters(template string) ([]string, error) {
	t.Logger.Log(3, "listing parameters for %s", path.Join(t.BaseDir, template))
	fi, err := ioutil.ReadDir(path.Join(t.BaseDir, template))
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(fi))
	for _, f := range fi {
		t.Logger.Log(3, "Found name %s with ext %s and base %s", f.Name(), path.Ext(f.Name()), path.Base(f.Name()))
		if path.Ext(f.Name()) == ".json" {
			names = append(names, strings.TrimSuffix(path.Base(f.Name()), path.Ext(f.Name())))
		}
	}
	return names, nil
}

func (t *TemplateFinder) ParameterFilename(template string, params string) string {
	return path.Join(t.BaseDir, template, params + ".json")
}