package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// A not-found answer names the id that was asked for, and a call that left the
// id out (or used the wrong key) says the argument is required instead of
// printing "workflow  not found".
func TestAdaptedToolNotFoundNamesTheRequestedID(t *testing.T) {
	s, _ := twoProfileServer(t)
	for _, c := range []struct{ tool, key string }{
		{"workflow_delete", "workflow_id"},
		{"person_get", "person_id"},
		{"secret_delete", "id"},
	} {
		_, err := callToolIn(context.Background(), s, nameA, c.tool, json.RawMessage(`{"`+c.key+`":"no-such-id"}`))
		if err == nil || !strings.Contains(err.Error(), "no-such-id") || !strings.Contains(err.Error(), "not found") {
			t.Errorf("%s with an unknown id: %v, want a not-found error naming it", c.tool, err)
		}
		_, err = callToolIn(context.Background(), s, nameA, c.tool, json.RawMessage(`{"wrong_key":"x"}`))
		if err == nil || !strings.Contains(err.Error(), `"`+c.key+`" argument is required`) {
			t.Errorf("%s without %s: %v, want a required-argument error", c.tool, c.key, err)
		}
	}
}
