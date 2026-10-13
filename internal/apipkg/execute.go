package apipkg

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// httpClient sends every package request (tests swap it).
var httpClient = &http.Client{Timeout: 60 * time.Second}

// Response-size caps, as internal/nodes/service: no body is read unbounded,
// and a non-2xx body echoed into an error is capped tighter.
const (
	maxResponseBody  = 64 << 20 // 64 MiB
	maxErrorBodyEcho = 64 << 10 // 64 KiB
)

// Node is the workflow node of one package.
type Node struct {
	pkg *Package
}

// Type is the package's node type.
func (n *Node) Type() string { return n.pkg.NodeTypeOf() }

// PerItemConfigFields holds back every op field from the engine's
// first-item resolution: the node resolves them once per input item.
func (n *Node) PerItemConfigFields() []string { return n.pkg.fieldKeys() }

// Execute runs the configured operation once per input item.
func (n *Node) Execute(ctx context.Context, input workflow.NodeInput, config map[string]any) ([]workflow.NodeOutput, error) {
	return run(ctx, n.pkg, input, config, true)
}

// ExecuteExtension runs a package that extends nativeType (e.g.
// "service.github") for a native node whose operation switch did not handle
// the config. It errors when no package extends nativeType.
func ExecuteExtension(ctx context.Context, nativeType string, input workflow.NodeInput, config map[string]any) ([]workflow.NodeOutput, error) {
	pkg := extensionFor(nativeType)
	if pkg == nil {
		return nil, fmt.Errorf("apipkg: no package extends %s", nativeType)
	}
	return run(ctx, pkg, input, config, false)
}

// HasExtension reports whether a package extends nativeType with
// (resource, operation). An empty resource matches any package op with
// that operation name and no resource.
func HasExtension(nativeType, resource, operation string) bool {
	pkg := extensionFor(nativeType)
	if pkg == nil {
		return false
	}
	_, ok := pkg.Op(resource, operation)
	return ok
}

// fieldKeys is the union of every op's field keys, sorted.
func (p *Package) fieldKeys() []string {
	seen := map[string]bool{}
	var out []string
	for _, o := range p.Operations {
		for _, f := range o.Fields {
			if !seen[f.Key] {
				seen[f.Key] = true
				out = append(out, f.Key)
			}
		}
	}
	sort.Strings(out)
	return out
}

// opRun is one operation run for one input item.
type opRun struct {
	pkg     *Package
	op      *Operation
	params  map[string]any
	item    workflow.Item
	credDef *Credential
	scope   Scope
}

func (r *opRun) errf(format string, a ...any) error {
	return fmt.Errorf("apipkg %s %s: %w", r.pkg.ID, r.op.Key(), fmt.Errorf(format, a...))
}

// run executes pkg for every input item. perItem resolves held-back
// workflow expressions per item (a package node holds its fields back).
func run(ctx context.Context, pkg *Package, input workflow.NodeInput, config map[string]any, perItem bool) ([]workflow.NodeOutput, error) {
	cred, _ := config["credential"].(map[string]any)
	if pkg.needsCredential() && len(cred) == 0 {
		return nil, fmt.Errorf("apipkg %s: no credential: set credential_id to a %s connection (monoagentcli connect %s)",
			pkg.ID, pkg.PrimaryPlatform(), pkg.PrimaryPlatform())
	}
	credDef := pkg.selectCredential(cred)
	items := input.Items
	if len(items) == 0 {
		items = []workflow.Item{{JSON: map[string]any{}}}
	}
	var held []string
	if perItem {
		held = pkg.fieldKeys()
	}
	engine := workflow.NewExpressionEngine()
	var out []workflow.Item
	for i, it := range items {
		if it.JSON == nil {
			it.JSON = map[string]any{}
		}
		params, err := itemParams(engine, config, held, it, input)
		if err != nil {
			return nil, fmt.Errorf("apipkg %s: item %d: %w", pkg.ID, i, err)
		}
		op, err := pkg.resolveOp(params)
		if err != nil {
			return nil, err
		}
		applyDefaults(op, params)
		if err := checkRequired(pkg, op, params); err != nil {
			return nil, err
		}
		r := &opRun{pkg: pkg, op: op, params: params, item: it, credDef: credDef,
			scope: Scope{P: params, Cred: cred, Item: it.JSON}}
		res, err := r.execute(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, res...)
	}
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}

// itemParams copies config, resolving the held-back keys against item.
func itemParams(engine *workflow.ExpressionEngine, config map[string]any, held []string, it workflow.Item, input workflow.NodeInput) (map[string]any, error) {
	params := make(map[string]any, len(config))
	for k, v := range config {
		params[k] = v
	}
	if len(held) == 0 {
		return params, nil
	}
	sub := map[string]any{}
	for _, k := range held {
		if v, ok := config[k]; ok {
			sub[k] = v
		}
	}
	resolved, err := engine.ResolveConfig(sub, workflow.ExpressionContext{
		JSON: it.JSON, Node: input.NodeOutputs, WorkflowID: input.WorkflowID, ExecutionID: input.ExecutionID,
	})
	if err != nil {
		return nil, err
	}
	for k, v := range resolved {
		params[k] = v
	}
	return params, nil
}

// resolveOp finds the op named by config resource/operation, defaulting to
// the first resource and its first operation (n8n leaves defaults unsaved).
func (p *Package) resolveOp(params map[string]any) (*Operation, error) {
	res, _ := params["resource"].(string)
	if res == "" {
		res = p.DefaultResource()
	}
	opName, _ := params["operation"].(string)
	if opName == "" {
		opName = p.DefaultOperation(res)
	}
	op, ok := p.Op(res, opName)
	if !ok {
		var have []string
		for _, o := range p.Operations {
			have = append(have, o.Key())
		}
		key := opName
		if res != "" {
			key = res + "." + opName
		}
		return nil, fmt.Errorf("apipkg %s: no operation %q (have: %s)", p.ID, key, strings.Join(have, ", "))
	}
	return op, nil
}

// applyDefaults fills unset params from the op's field defaults.
func applyDefaults(op *Operation, params map[string]any) {
	for _, f := range op.Fields {
		if _, ok := params[f.Key]; !ok && f.Default != nil {
			params[f.Key] = f.Default
		}
	}
}

func checkRequired(pkg *Package, op *Operation, params map[string]any) error {
	for _, f := range op.Fields {
		if !f.Required {
			continue
		}
		v := params[f.Key]
		if m, ok := v.(map[string]any); ok && m["__rl"] != nil {
			v = m["value"]
		}
		if isEmpty(v) {
			return fmt.Errorf("apipkg %s %s: field %q (%s) is required", pkg.ID, op.Key(), f.Key, f.Label)
		}
	}
	return nil
}

// execute sends the request (all pages) and turns the response into items.
func (r *opRun) execute(ctx context.Context) ([]workflow.Item, error) {
	if r.op.Pagination != nil {
		results, truncated, err := r.paginate(ctx)
		if err != nil {
			return nil, err
		}
		items := toItems(results, true)
		if truncated {
			for i := range items {
				items[i].JSON["pagination_truncated"] = true
			}
		}
		return items, nil
	}
	resp, err := r.send(ctx, pageParams{})
	if err != nil {
		return nil, err
	}
	if err := r.runAlso(ctx, resp); err != nil {
		return nil, err
	}
	if b := r.op.Binary; b != nil && b.Download {
		return []workflow.Item{r.downloadItem(resp)}, nil
	}
	if len(strings.TrimSpace(string(resp.body))) == 0 {
		return []workflow.Item{{JSON: map[string]any{"success": true}}}, nil
	}
	v := decodeBody(resp.body)
	if r.op.Output != nil && r.op.Output.Path != "" {
		v = lookupPath(v, strings.Split(r.op.Output.Path, "."))
	}
	split := r.op.Output == nil || r.op.Output.Split == nil || *r.op.Output.Split
	return toItems(v, split), nil
}

// response is a 2xx response.
type response struct {
	status int
	header http.Header
	body   []byte
	url    string
}

// send builds and sends one request; non-2xx responses are errors.
func (r *opRun) send(ctx context.Context, pp pageParams) (*response, error) {
	b, err := r.buildRequest(ctx, pp)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(b.req)
	if err != nil {
		return nil, r.errf("%s %s: %w", b.req.Method, b.req.URL.Redacted(), err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody+1))
	if err != nil {
		return nil, r.errf("reading response: %w", err)
	}
	if len(body) > maxResponseBody {
		return nil, r.errf("response body exceeds %d MiB", maxResponseBody>>20)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, r.errf("HTTP %d: %s", resp.StatusCode, errorBodyEcho(body))
	}
	return &response{status: resp.StatusCode, header: resp.Header, body: body, url: b.req.URL.String()}, nil
}

func errorBodyEcho(b []byte) string {
	if len(b) > maxErrorBodyEcho {
		return string(b[:maxErrorBodyEcho]) + fmt.Sprintf("…(%d bytes truncated)", len(b)-maxErrorBodyEcho)
	}
	return string(b)
}

// downloadItem stores a file response on the item's binary data.
func (r *opRun) downloadItem(resp *response) workflow.Item {
	prop := "data"
	if f := r.op.Binary.PropertyField; f != "" {
		if s := stringify(r.params[f]); s != "" {
			prop = s
		}
	}
	ct := resp.header.Get("Content-Type")
	name := ""
	if _, params, err := mime.ParseMediaType(resp.header.Get("Content-Disposition")); err == nil {
		name = params["filename"]
	}
	return workflow.Item{
		JSON: map[string]any{
			"mime_type": ct, "file_name": name, "size": len(resp.body), "binary_property": prop,
		},
		Binary: map[string][]byte{prop: resp.body},
	}
}

// decodeBody parses JSON, falling back to the text.
func decodeBody(b []byte) any {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return string(b)
	}
	return v
}

// toItems turns a value into items: an array splits into one item per
// element (when split), an object is one item, anything else is
// {"data": value}.
func toItems(v any, split bool) []workflow.Item {
	if arr, ok := v.([]any); ok && split {
		items := make([]workflow.Item, 0, len(arr))
		for _, el := range arr {
			items = append(items, workflow.Item{JSON: asObject(el)})
		}
		return items
	}
	if v == nil {
		return []workflow.Item{}
	}
	return []workflow.Item{{JSON: asObject(v)}}
}

func asObject(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{"data": v}
}

// runAlso sends the operation's follow-up requests in order, each with res
// bound to the main response. A step whose "when" renders empty is skipped.
func (r *opRun) runAlso(ctx context.Context, main *response) error {
	if len(r.op.Also) == 0 {
		return nil
	}
	var res any
	if len(strings.TrimSpace(string(main.body))) > 0 {
		res = decodeBody(main.body)
	}
	for i, st := range r.op.Also {
		sc := r.scope
		sc.Res = res
		if st.When != "" {
			w, err := renderString(st.When, sc)
			if err != nil {
				return r.errf("also[%d] when: %w", i, err)
			}
			if isEmpty(w) || w == false {
				continue
			}
		}
		op := *r.op
		op.Method, op.Path, op.BaseURL = st.Method, st.Path, st.BaseURL
		op.Query, op.Body, op.BodyType, op.Headers = st.Query, st.Body, st.BodyType, st.Headers
		op.Pagination, op.Binary, op.Also = nil, nil, nil
		step := *r
		step.op, step.scope = &op, sc
		if _, err := step.send(ctx, pageParams{}); err != nil {
			return fmt.Errorf("also[%d]: %w", i, err)
		}
	}
	return nil
}
