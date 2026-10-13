package org

import (
	"encoding/json"
	"fmt"

	"github.com/monoes/mono-agent/internal/cli/orgenv"
	"github.com/monoes/mono-agent/internal/orggrant"
)

// Deps is what the org commands need from the CLI.
type Deps struct {
	// InvalidInput builds the CLI's invalid-input error (exit code 3).
	InvalidInput func(format string, a ...interface{}) error
}

// orgEnv is the environment every org command runs in: the org root and
// profile (orgenv.Env, shared by all the org commands of one invocation)
// and the CLI's Deps.
type orgEnv struct {
	*orgenv.Env
	d Deps
}

func newEnv(d Deps, e *orgenv.Env) *orgEnv { return &orgEnv{Env: e, d: d} }

// genOptions returns what reconcile needs to write generated org JSON
// blocks for this process.
func (e *orgEnv) genOptions(profileID string) orggrant.GenOptions { return e.GenOptions(profileID) }

// printOrgJSON writes a raw JSON payload to stdout with a trailing newline.
func printOrgJSON(payload []byte) error {
	_, err := fmt.Println(string(payload))
	return err
}

// printJSONValue marshals v and prints it as one line.
func printJSONValue(v interface{}) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return printOrgJSON(b)
}
