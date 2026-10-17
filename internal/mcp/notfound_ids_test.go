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

// "reports_to" is required but "" is a real value: it makes a role the root of
// its org. The required-argument check must let an empty string through for it
// (and still refuse a call that leaves the key out or gives a non-string).
func TestReportsToMayBeEmptyButNotMissing(t *testing.T) {
	reached := false
	next := func(ctx context.Context, s *Server, args json.RawMessage) (interface{}, error) {
		reached = true
		return nil, nil
	}
	schema := objSchema(map[string]interface{}{
		"org_name":   strParam("org"),
		"role_id":    strParam("role"),
		"reports_to": strParam("manager id, or empty for the root"),
	}, "org_name", "role_id", "reports_to")
	h := requireStringArgs(schema, next)

	reached = false
	if _, err := h(context.Background(), nil, json.RawMessage(`{"org_name":"o","role_id":"r","reports_to":""}`)); err != nil || !reached {
		t.Errorf(`reports_to:"" must reach the store, got err=%v reached=%v`, err, reached)
	}
	for name, args := range map[string]string{
		"key left out":   `{"org_name":"o","role_id":"r"}`,
		"not a string":   `{"org_name":"o","role_id":"r","reports_to":null}`,
		"empty role_id":  `{"org_name":"o","role_id":"","reports_to":"x"}`,
		"empty org_name": `{"org_name":"","role_id":"r","reports_to":""}`,
		"wrong type":     `{"org_name":"o","role_id":"r","reports_to":5}`,
	} {
		reached = false
		_, err := h(context.Background(), nil, json.RawMessage(args))
		if err == nil || reached || !strings.Contains(err.Error(), "argument is required") {
			t.Errorf("%s: err=%v reached=%v, want a required-argument error and no store call", name, err, reached)
		}
	}
}

// The real org tools: an empty reports_to gets past the argument check (the
// error, if any, comes from the org lookup, not from "argument is required").
func TestOrgRoleToolsAcceptAnEmptyReportsTo(t *testing.T) {
	s, _ := twoProfileServer(t)
	for _, c := range []struct{ tool, args string }{
		{"org_role_set_reports_to", `{"org_name":"no-such-org","role_id":"r","reports_to":""}`},
		{"org_role_add", `{"org_name":"no-such-org","title":"Boss","reports_to":""}`},
	} {
		_, err := callToolIn(context.Background(), s, nameA, c.tool, json.RawMessage(c.args))
		if err != nil && strings.Contains(err.Error(), "argument is required") {
			t.Errorf("%s with reports_to:\"\" was refused as missing: %v", c.tool, err)
		}
	}
}
