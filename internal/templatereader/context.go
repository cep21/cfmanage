package templatereader

import (
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/pkg/errors"
)

// Ctx contains fun helper functions that make template generation easier
type Ctx struct{}

// Env calls out to os.Getenv
func (t Ctx) Env(key string) string {
	return os.Getenv(key)
}

// File loads a filename into the template
func (t Ctx) File(key string) (string, error) {
	b, err := ioutil.ReadFile(key)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// JSON converts a string into a JSON string
func (t Ctx) JSON(key string) (string, error) {
	res, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	return string(res), nil
}

// JSONStr converts a string into a JSON string, but does not return the starting and ending "
// This lets you use a JSON template that is itself still JSON
func (t Ctx) JSONStr(key string) (string, error) {
	res, err := json.Marshal(key)
	if err != nil {
		return "", err
	}
	if len(res) < 2 {
		return "", errors.Errorf("Invalid json str %s", res)
	}
	if res[0] != '"' || res[len(res)-1] != '"' {
		return "", errors.Errorf("Invalid json str quotes %s", res)
	}
	return string(res[1 : len(res)-1]), nil
}

// MustEnv is like Env, but will error if the env variable is empty
func (t Ctx) MustEnv(key string) (string, error) {
	if ret := t.Env(key); ret != "" {
		return ret, nil
	}
	return "", errors.Errorf("Unable to find environment variable %s", key)
}
