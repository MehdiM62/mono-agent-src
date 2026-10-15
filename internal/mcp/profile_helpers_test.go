package mcp

import (
	"context"
	"encoding/json"
)

// testProfile is the profile the test helpers name when a test does not say
// which one it acts on. The server holds no profile, so every scoped call has to
// carry one; tests of the missing-profile error build their request with
// request() instead of callToolReq().
const testProfile = "default"

// withTestProfile adds the profile argument to args (a map, nil or raw JSON
// object) unless it already has one.
func withTestProfile(profile string, args interface{}) interface{} {
	switch a := args.(type) {
	case nil:
		return map[string]interface{}{profileArg: profile}
	case map[string]interface{}:
		if _, ok := a[profileArg]; ok {
			return a
		}
		out := make(map[string]interface{}, len(a)+1)
		for k, v := range a {
			out[k] = v
		}
		out[profileArg] = profile
		return out
	case json.RawMessage:
		return rawWithProfile(profile, a)
	case []byte:
		return rawWithProfile(profile, a)
	case string:
		return rawWithProfile(profile, json.RawMessage(a))
	}
	return args
}

func rawWithProfile(profile string, raw json.RawMessage) json.RawMessage {
	m := map[string]json.RawMessage{}
	if len(raw) > 0 && json.Unmarshal(raw, &m) != nil {
		return raw
	}
	if _, ok := m[profileArg]; !ok {
		b, _ := json.Marshal(profile)
		m[profileArg] = b
	}
	out, _ := json.Marshal(m)
	return out
}

// callToolIn is callTool for a call that acts on profile.
func callToolIn(ctx context.Context, s *Server, profile, name string, args json.RawMessage) (string, error) {
	return callTool(ctx, s, name, rawWithProfile(profile, args))
}

// callToolDefault is callTool for a call that acts on testProfile.
func callToolDefault(ctx context.Context, s *Server, name string, args json.RawMessage) (string, error) {
	return callToolIn(ctx, s, testProfile, name, args)
}

// serverTestProfile is the profile a test fixture's server was built "for": its
// Options.Profile (which a non-grant server ignores) or testProfile.
func serverTestProfile(s *Server) string {
	if s.opts.Profile != "" {
		return s.opts.Profile
	}
	return testProfile
}
