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
	if credDef != nil && credDef.ScopeGroup != "" && ScopeCheck != nil {
		if err := ScopeCheck(cred, credDef.ScopeGroup); err != nil {
			return nil, fmt.Errorf("apipkg %s: %w", pkg.ID, err)
		}
	}
	if credDef != nil && credDef.hasMethod("client_credentials") && isEmpty(cred["access_token"]) {
		tok, err := clientCredentialsToken(ctx, credDef, cred)
		if err != nil {
			return nil, fmt.Errorf("apipkg %s: %w", pkg.ID, err)
		}
		withTok := make(map[string]any, len(cred)+1)
		for k, v := range cred {
			withTok[k] = v
		}
		withTok["access_token"] = tok
		cred = withTok
	}
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
		if op.Binary != nil && op.Binary.UploadField != "" {
			r.scope.Bin = r.binMeta()
		}
		if op.Batch {
			all := make([]any, 0, len(items))
			for _, x := range items {
				if x.JSON == nil {
					all = append(all, map[string]any{})
					continue
				}
				all = append(all, x.JSON)
			}
			r.scope.Items = all
		}
		res, err := r.execute(ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, res...)
		if op.Batch {
			break // one request covered every item
		}
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
	for i, st := range r.op.Before {
		resp, err := r.sendStep(ctx, st, r.scope)
		if err != nil {
			return nil, fmt.Errorf("before[%d]: %w", i, err)
		}
		var v any
		if resp != nil && len(strings.TrimSpace(string(resp.body))) > 0 {
			v = decodeBody(resp.body)
		}
		r.scope.Pres = append(r.scope.Pres, v)
		r.scope.Pre = v
	}
	if r.op.Pagination != nil {
		results, truncated, err := r.paginate(ctx)
		if err != nil {
			return nil, err
		}
		shaped, err := r.shapeOutput(ctx, results)
		if err != nil {
			return nil, err
		}
		items := toItems(shaped, true)
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
	if r.downloads() {
		return []workflow.Item{r.downloadItem(resp)}, nil
	}
	if len(strings.TrimSpace(string(resp.body))) == 0 {
		return []workflow.Item{{JSON: map[string]any{"success": true}}}, nil
	}
	v := decodeBody(resp.body)
	if path := r.outputPath(); path != "" {
		v = lookupPath(v, strings.Split(path, "."))
	}
	if r.op.Output != nil && r.op.Output.HeaderRow {
		v = rowsToObjects(v)
	}
	if v, err = r.shapeOutput(ctx, v); err != nil {
		return nil, err
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
		if _, err := r.sendStep(ctx, st, sc); err != nil {
			return fmt.Errorf("also[%d]: %w", i, err)
		}
	}
	return nil
}

// ScopeCheck, when set, checks that a credential grants an umbrella scope
// group (see Credential.ScopeGroup). It is wired to
// connections.CheckCredentialScopes by the registry.
var ScopeCheck func(cred map[string]any, group string) error

// sendStep sends one extra request (before/also) of the operation with
// scope sc and the operation's auth.
func (r *opRun) sendStep(ctx context.Context, st Step, sc Scope) (*response, error) {
	if st.When != "" {
		w, err := renderString(st.When, sc)
		if err != nil {
			return nil, r.errf("when: %w", err)
		}
		if isEmpty(w) || w == false {
			return nil, nil
		}
	}
	op := *r.op
	op.Method, op.Path, op.BaseURL = st.Method, st.Path, st.BaseURL
	op.Query, op.Body, op.BodyType, op.Headers = st.Query, st.Body, st.BodyType, st.Headers
	op.Pagination, op.Binary, op.Also, op.Before = nil, nil, nil, nil
	step := *r
	step.op, step.scope = &op, sc
	return step.send(ctx, pageParams{})
}

// shapeOutput applies output.each, output.expand and output.match to a
// result list.
func (r *opRun) shapeOutput(ctx context.Context, v any) (any, error) {
	o := r.op.Output
	if o == nil {
		return v, nil
	}
	list, isList := v.([]any)
	if !isList {
		// A single object is shaped as a one-element list (Coda getRow).
		if _, isObj := v.(map[string]any); !isObj {
			return v, nil
		}
		shaped, err := r.shapeOutput(ctx, []any{v})
		if err != nil {
			return nil, err
		}
		if l := shaped.([]any); len(l) == 1 {
			return l[0], nil
		} else if len(l) == 0 {
			return nil, nil
		}
		return shaped, nil
	}
	each, err := r.renderOutputKey(o.Each)
	if err != nil {
		return nil, r.errf("output.each: %w", err)
	}
	if each != "" {
		path := strings.Split(each, ".")
		out := make([]any, 0, len(list))
		for _, el := range list {
			if x := lookupPath(el, path); x != nil {
				out = append(out, x)
			}
		}
		list = out
	}
	if st := o.Expand; st != nil {
		for i, el := range list {
			sc := r.scope
			sc.El = el
			resp, err := r.sendStep(ctx, *st, sc)
			if err != nil {
				return nil, fmt.Errorf("output.expand element %d: %w", i, err)
			}
			if resp != nil && len(strings.TrimSpace(string(resp.body))) > 0 {
				list[i] = decodeBody(resp.body)
			}
		}
	}
	if m := o.Match; m != nil {
		rowsV, err := renderString(m.Rows, r.scope)
		if err != nil {
			return nil, r.errf("output.match.rows: %w", err)
		}
		conds, _ := rowsV.([]any)
		if len(conds) > 0 {
			combine, err := renderText(m.Combine, r.scope)
			if err != nil {
				return nil, r.errf("output.match.combine: %w", err)
			}
			first, err := renderString(m.First, r.scope)
			if err != nil {
				return nil, r.errf("output.match.first: %w", err)
			}
			or := strings.EqualFold(combine, "OR")
			kept := make([]any, 0, len(list))
			for _, el := range list {
				if matchRow(el, conds, m.Column, m.Value, or) {
					kept = append(kept, el)
					if b, _ := toBool(first); b == true {
						break
					}
				}
			}
			list = kept
		}
	}
	if o.Firestore != "" {
		on, err := renderString(o.Firestore, r.scope)
		if err != nil {
			return nil, r.errf("output.firestore: %w", err)
		}
		if b, _ := toBool(on); b == true {
			for i, el := range list {
				list[i] = firestoreDoc(el)
			}
		}
	}
	if o.To != nil {
		apply := true
		if o.ToWhen != "" {
			w, err := renderString(o.ToWhen, r.scope)
			if err != nil {
				return nil, r.errf("output.to_when: %w", err)
			}
			apply = !isEmpty(w) && w != false
		}
		if apply {
			for i, el := range list {
				sc := r.scope
				sc.El = el
				x, err := renderValue(o.To, sc)
				if err != nil {
					return nil, r.errf("output.to element %d: %w", i, err)
				}
				list[i] = x
			}
		}
	}
	return list, nil
}

// matchRow reports whether el satisfies the condition rows (each names a
// field via colKey and a wanted value via valKey), all (AND) or any (OR).
func matchRow(el any, conds []any, colKey, valKey string, or bool) bool {
	obj, _ := el.(map[string]any)
	for _, c := range conds {
		cm, _ := c.(map[string]any)
		ok := stringify(obj[stringify(cm[colKey])]) == stringify(cm[valKey])
		if or && ok {
			return true
		}
		if !or && !ok {
			return false
		}
	}
	return !or
}

// renderOutputKey renders an output path/each value, which may be a
// template over p (n8n rawData/simple toggles); empty means none.
func (r *opRun) renderOutputKey(s string) (string, error) {
	if !hasTemplate(s) {
		return s, nil
	}
	return renderText(s, r.scope)
}

// outputPath is the rendered output.path ("" when unset or empty).
func (r *opRun) outputPath() string {
	if r.op.Output == nil {
		return ""
	}
	p, err := r.renderOutputKey(r.op.Output.Path)
	if err != nil {
		return r.op.Output.Path
	}
	return p
}

// downloads reports whether this run's response is a file.
func (r *opRun) downloads() bool {
	b := r.op.Binary
	if b == nil {
		return false
	}
	if b.DownloadWhen == "" {
		return b.Download
	}
	w, err := renderString(b.DownloadWhen, r.scope)
	return err == nil && !isEmpty(w) && w != false
}
