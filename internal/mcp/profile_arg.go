package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

// profileArg is the name of the argument every profile-scoped tool requires.
// The server holds no profile of its own: one server serves every profile,
// and each call says which one it acts on.
const profileArg = "profile"

type profileCtxKey struct{}

// withProfile returns ctx carrying the profile id a call was resolved to.
func withProfile(ctx context.Context, profileID string) context.Context {
	return context.WithValue(ctx, profileCtxKey{}, profileID)
}

// profileOf is the profile id of the call ctx belongs to. It is empty only for
// a global tool, which reads no profile data; a scoped handler always has one,
// because callToolResult refuses a scoped call that names none.
func profileOf(ctx context.Context) string {
	id, _ := ctx.Value(profileCtxKey{}).(string)
	return id
}

// scoped is the one place a tool descriptor learns about profiles. Every tool
// the server lists goes through it (allTools, workflowTools, servedTools): a
// tool is profile-scoped unless it sets global, and a scoped tool gets the
// required `profile` argument in its input schema, so a new tool cannot forget
// it. The descriptor's own schema is copied, not edited.
func scoped(tools []tool) []tool {
	out := make([]tool, len(tools))
	for i, t := range tools {
		if !t.global {
			t.schema = withProfileProperty(t.schema, true)
		}
		out[i] = t
	}
	return out
}

// withProfileProperty returns a copy of an object schema with the profile
// property added, and listed in required when required is true.
func withProfileProperty(schema map[string]interface{}, required bool) map[string]interface{} {
	out := make(map[string]interface{}, len(schema)+2)
	for k, v := range schema {
		out[k] = v
	}
	props := map[string]interface{}{}
	if old, ok := schema["properties"].(map[string]interface{}); ok {
		for k, v := range old {
			props[k] = v
		}
	}
	desc := "Profile this call acts on, by id or name. Required: the server holds no active profile. Every workflow, person, secret, task, HIL item and publication of another profile is not found."
	if !required {
		desc = "Profile this server's grant belongs to, by id or name. Optional: the grant fixes the profile, and a different one is refused."
	}
	props[profileArg] = strParam(desc)
	out["properties"] = props
	if required {
		var req []string
		switch r := schema["required"].(type) {
		case []string:
			req = r
		case []interface{}:
			for _, v := range r {
				if s, ok := v.(string); ok {
					req = append(req, s)
				}
			}
		}
		for _, r := range req {
			if r == profileArg {
				return out
			}
		}
		out["required"] = append([]string{profileArg}, req...)
	}
	return out
}

// splitProfileArg reads the profile argument of a call and returns the
// arguments without it, so a handler that treats its whole argument object as
// data (a workflow's trigger input) never sees it. present is false when the
// argument is missing, null or not a string; the value is trimmed.
func splitProfileArg(args json.RawMessage) (profile string, rest json.RawMessage, present bool, err error) {
	trimmed := bytes.TrimSpace(args)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return "", args, false, nil
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &m); err != nil {
		return "", nil, false, fmt.Errorf("invalid arguments: %w", err)
	}
	raw, ok := m[profileArg]
	if !ok {
		return "", args, false, nil
	}
	delete(m, profileArg)
	rest, err = json.Marshal(m)
	if err != nil {
		return "", nil, false, fmt.Errorf("invalid arguments: %w", err)
	}
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", rest, false, nil
	}
	s = strings.TrimSpace(s)
	return s, rest, s != "", nil
}

// profileList names the profiles a caller may pass: "Work (a1), Home (b2)".
func profileList(db *sql.DB) string {
	rows, err := db.Query(`SELECT id, name FROM profiles ORDER BY created_at, id`)
	if err != nil {
		return ""
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil {
			parts = append(parts, fmt.Sprintf("%s (%s)", name, id))
		}
	}
	return strings.Join(parts, ", ")
}

// profileHint is the sentence that ends a profile error: the profiles that
// exist, or a note that none does.
func profileHint(db *sql.DB) string {
	if list := profileList(db); list != "" {
		return " Profiles: " + list
	}
	return " No profile exists yet; create one with `monoagentcli profile create`."
}

// resolveCallProfile turns the `profile` argument of a call into a profile id
// and returns the arguments without it. A missing, unknown or ambiguous profile
// is an invalid_input error that lists the profiles, never a guess.
func resolveCallProfile(db *sql.DB, args json.RawMessage) (string, json.RawMessage, error) {
	name, rest, present, err := splitProfileArg(args)
	if err != nil {
		return "", nil, err
	}
	if !present {
		return "", nil, fmt.Errorf("invalid_input: the %q argument is required: pass the id or name of the profile this call acts on.%s", profileArg, profileHint(db))
	}
	id, err := resolveProfileID(db, name)
	if err != nil {
		return "", nil, err
	}
	return id, rest, nil
}

// resolveProfileID accepts a profile's id or its name and returns the id: the
// rules of the CLI's --profile (an id, else a name without regard to case; the
// reserved machine scope is never an id). A name that matches several profiles
// is an error: it never picks one.
func resolveProfileID(db *sql.DB, idOrName string) (string, error) {
	const machineScope = "_machine"
	var id string
	if idOrName != machineScope {
		if err := db.QueryRow(`SELECT id FROM profiles WHERE id = ?`, idOrName).Scan(&id); err == nil {
			return id, nil
		}
	}
	rows, err := db.Query(`SELECT id FROM profiles WHERE LOWER(name) = LOWER(?) ORDER BY created_at, id`, idOrName)
	if err != nil {
		return "", fmt.Errorf("invalid_input: look up profile %q: %v", idOrName, err)
	}
	var ids []string
	for rows.Next() {
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	switch len(ids) {
	case 1:
		return ids[0], nil
	case 0:
		return "", fmt.Errorf("invalid_input: profile %q not found (checked both id and name).%s", idOrName, profileHint(db))
	}
	return "", fmt.Errorf("invalid_input: profile name %q matches %d profiles (%s): pass the id.%s", idOrName, len(ids), strings.Join(ids, ", "), profileHint(db))
}

// scopeCall resolves the profile of a scoped tool call: it returns ctx carrying
// the profile id (see profileOf) and the arguments without the profile.
func (s *Server) scopeCall(ctx context.Context, args json.RawMessage) (context.Context, json.RawMessage, error) {
	rt, err := s.runtime()
	if err != nil {
		return ctx, nil, err
	}
	id, rest, err := resolveCallProfile(rt.db.DB, args)
	if err != nil {
		return ctx, nil, err
	}
	return withProfile(ctx, id), rest, nil
}
