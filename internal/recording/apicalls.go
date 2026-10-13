package recording

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// API calls captured while recording (`record api`): the page's JSON
// fetch/XHR calls, and http.request node configs / a small workflow built
// from them. Config keys are the ones internal/nodes/http RequestNode
// reads (method, url, headers, body_type, body, auth_type, auth_token,
// response_format). A credential the page sent becomes a "@secret:<name>"
// reference the person fills in the vault; its value was never recorded.

// APICall is one captured JSON call, numbered from 1 in recording order.
type APICall struct {
	N int `json:"n"`
	NetEntry
}

// LoadNetwork reads a recording's network.jsonl; a recording without one
// has no calls.
func LoadNetwork(dir string) ([]NetEntry, error) {
	f, err := os.Open(filepath.Join(dir, NetworkArtifact))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []NetEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var e NetEntry
		if json.Unmarshal(sc.Bytes(), &e) == nil {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}

// APICalls keeps the calls answered with JSON, dropping exact repeats
// (same method, URL and body: a page polling the same endpoint).
func APICalls(entries []NetEntry) []APICall {
	seen := map[string]bool{}
	var out []APICall
	for _, e := range entries {
		if !strings.Contains(strings.ToLower(e.ContentType), "json") {
			continue
		}
		k := e.Method + " " + e.URL + " " + e.RequestBody
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, APICall{N: len(out) + 1, NetEntry: e})
	}
	return out
}

// Headers the HTTP client sets itself, or that only a browser may send.
var skipHeaders = map[string]bool{
	"content-length": true, "host": true, "connection": true, "accept-encoding": true,
	"user-agent": true, "origin": true, "referer": true, "cookie": true,
	"content-type": true, // body_type sets it
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
}

// SecretName is the vault entry a call's credential header is read from:
// "<host>-<header>", e.g. "api-example-com-authorization".
func SecretName(rawURL, header string) string {
	host := ""
	if u, err := url.Parse(rawURL); err == nil {
		host = u.Hostname()
	}
	return strings.Trim(slug(host)+"-"+slug(header), "-")
}

// HTTPNode is one http.request node built from a call.
type HTTPNode struct {
	Name   string         `json:"name"`
	Config map[string]any `json:"config"`
	// Secrets are the vault entries the config references.
	Secrets []string `json:"secrets,omitempty"`
}

// BuildHTTPNode turns a call into an http.request node config.
func BuildHTTPNode(c APICall) HTTPNode {
	cfg := map[string]any{
		"method":          c.Method,
		"url":             c.URL,
		"response_format": "json",
	}
	var secrets []string
	headers := map[string]any{}
	for _, h := range c.RequestHeaders {
		lower := strings.ToLower(h.Name)
		if skipHeaders[lower] {
			continue
		}
		if !h.Credential {
			headers[h.Name] = h.Value
			continue
		}
		name := SecretName(c.URL, h.Name)
		secrets = append(secrets, name)
		if lower == "authorization" && strings.EqualFold(h.Scheme, "bearer") {
			cfg["auth_type"] = "bearer"
			cfg["auth_token"] = "@secret:" + name
			continue
		}
		// The whole header value (scheme included) comes from the vault:
		// a reference must be the entire string to resolve.
		headers[h.Name] = "@secret:" + name
	}
	if len(headers) > 0 {
		cfg["headers"] = headers
	}
	if c.RequestBody != "" {
		switch {
		case strings.Contains(strings.ToLower(c.RequestType), "x-www-form-urlencoded"):
			cfg["body_type"] = "form"
		default:
			cfg["body_type"] = "json"
		}
		cfg["body"] = c.RequestBody
	}
	return HTTPNode{Name: callName(c), Config: cfg, Secrets: secrets}
}

// callName is a node name such as "POST /api/v1/items".
func callName(c APICall) string {
	p := c.URL
	if u, err := url.Parse(c.URL); err == nil && u.Path != "" {
		p = u.Path
	}
	name := c.Method + " " + p
	if r := []rune(name); len(r) > 60 {
		name = string(r[:59]) + "…"
	}
	return name
}

// ImportWorkflow is the `workflow import --file` shape (see examples/).
type ImportWorkflow struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Version     int                `json:"version"`
	IsActive    bool               `json:"is_active"`
	Nodes       []ImportNode       `json:"nodes"`
	Connections []ImportConnection `json:"connections"`
}

type ImportNode struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Name     string         `json:"name"`
	Position map[string]int `json:"position"`
	Disabled bool           `json:"disabled"`
	Config   map[string]any `json:"config"`
}

type ImportConnection struct {
	ID           string `json:"id"`
	Source       string `json:"source"`
	SourceHandle string `json:"source_handle"`
	Target       string `json:"target"`
	TargetHandle string `json:"target_handle"`
}

// cookieNote is said on every generated workflow: the browser added the
// page's cookies itself, so they were never recorded.
const cookieNote = "The page's cookies were not recorded; a call that relied on a browser login needs a credential header instead."

// BuildAPIWorkflow chains a manual trigger and one http.request node per
// call, in recording order.
func BuildAPIWorkflow(name string, calls []APICall) (*ImportWorkflow, []string) {
	wf := &ImportWorkflow{Name: name, Version: 1, Nodes: []ImportNode{{
		ID: "start", Type: "trigger.manual", Name: "Start", Position: map[string]int{"x": 0, "y": 0}, Config: map[string]any{},
	}}}
	var secrets []string
	seen := map[string]bool{}
	prev := "start"
	redacted := false
	for i, c := range calls {
		n := BuildHTTPNode(c)
		redacted = redacted || strings.Contains(c.URL, Redacted) || strings.Contains(c.RequestBody, Redacted)
		id := fmt.Sprintf("call_%d", c.N)
		wf.Nodes = append(wf.Nodes, ImportNode{ID: id, Type: "http.request", Name: n.Name,
			Position: map[string]int{"x": 320 * (i + 1), "y": 0}, Config: n.Config})
		wf.Connections = append(wf.Connections, ImportConnection{ID: prev + "-to-" + id, Source: prev,
			SourceHandle: "main", Target: id, TargetHandle: "main"})
		prev = id
		for _, s := range n.Secrets {
			if !seen[s] {
				seen[s] = true
				secrets = append(secrets, s)
			}
		}
	}
	desc := fmt.Sprintf("%d API call(s) captured while recording.", len(calls))
	if len(secrets) > 0 {
		desc += " Add these vault secrets before running: " + strings.Join(secrets, ", ") + "."
	}
	if redacted {
		desc += " Values the recorder withheld read " + Redacted + ": replace them (or point them at a vault secret) before running."
	}
	wf.Description = desc + " " + cookieNote
	return wf, secrets
}
