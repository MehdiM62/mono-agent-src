package httpnodes

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

// paginationModes are the pagination_type values http.request accepts.
//   - offset: the original mode — a page index (page_field, from 0) and
//     per_page=page_size in the query; a list body is split into items and
//     a short or empty page ends it.
//   - update_parameter: set query/header/body parameters on every request
//     from templates that see the previous response ($response) and the
//     number of pages fetched so far ($pageCount).
//   - next_url: the previous response names the next URL
//     (pagination_next_url, e.g. {{ $response.body.next }}); an empty
//     result ends it.
var paginationModes = []string{"none", "offset", "update_parameter", "next_url"}

var paginationModeAliases = map[string]string{
	"off":                           "none",
	"updateAParameterInEachRequest": "update_parameter",
	"responseContainsNextURL":       "next_url",
}

var completeWhenValues = []string{"response_is_empty", "status_codes", "expression"}

var completeWhenAliases = map[string]string{
	"responseIsEmpty":            "response_is_empty",
	"receiveSpecificStatusCodes": "status_codes",
	"other":                      "expression",
}

// defaultMaxPages is the page cap when max_pages is not set — the cap the
// offset mode has always had, so a misconfigured loop ends.
const defaultMaxPages = 1000

type paginationOptions struct {
	mode         string
	pageField    string
	pageSize     int
	maxPages     int
	interval     time.Duration
	completeWhen string
	statusCodes  map[int]bool
}

func parsePaginationOptions(config map[string]interface{}, node string) (paginationOptions, error) {
	p := paginationOptions{pageField: "page", pageSize: 100, maxPages: defaultMaxPages}
	wrap := func(err error) error { return fmt.Errorf("%s: %w", node, err) }
	var err error
	if p.mode, err = oneOf(config, "pagination_type", "none", paginationModes, paginationModeAliases); err != nil {
		return p, wrap(err)
	}
	if f := configString(config, "page_field"); f != "" {
		p.pageField = f
	}
	if n, ok, err := configNumber(config, "page_size"); err != nil {
		return p, wrap(err)
	} else if ok {
		p.pageSize = int(n)
	}
	if n, ok, err := configNumber(config, "max_pages"); err != nil {
		return p, wrap(err)
	} else if ok && n > 0 {
		p.maxPages = int(n)
	}
	if n, ok, err := configNumber(config, "pagination_interval_ms"); err != nil {
		return p, wrap(err)
	} else if ok && n > 0 {
		p.interval = time.Duration(n) * time.Millisecond
	}
	if p.completeWhen, err = oneOf(config, "pagination_complete_when", "response_is_empty", completeWhenValues, completeWhenAliases); err != nil {
		return p, wrap(err)
	}
	if p.completeWhen == "status_codes" {
		p.statusCodes = map[int]bool{}
		for _, s := range strings.FieldsFunc(stringifyValue(config["pagination_status_codes"]), func(r rune) bool { return r == ',' || r == ' ' || r == '[' || r == ']' }) {
			code, err := strconv.Atoi(s)
			if err != nil {
				return p, wrap(fmt.Errorf("pagination_status_codes: %q is not a status code", s))
			}
			p.statusCodes[code] = true
		}
		if len(p.statusCodes) == 0 {
			return p, wrap(fmt.Errorf("pagination_status_codes: list the status codes that end pagination, e.g. 404"))
		}
	}
	if p.completeWhen == "expression" && p.mode != "none" && p.mode != "offset" && strings.TrimSpace(configString(config, "pagination_complete_expression")) == "" {
		return p, wrap(fmt.Errorf("pagination_complete_expression is required when pagination_complete_when is expression"))
	}
	if p.mode == "next_url" && strings.TrimSpace(configString(config, "pagination_next_url")) == "" {
		return p, wrap(fmt.Errorf("pagination_next_url is required for pagination_type next_url, e.g. {{ $response.body.next }}"))
	}
	return p, nil
}

// paginationParam is one pagination_parameters row.
type paginationParam struct{ in, name, value string }

func parsePaginationParams(raw interface{}) ([]paginationParam, error) {
	if s, ok := raw.(string); ok && strings.TrimSpace(s) != "" {
		var decoded interface{}
		if err := json.Unmarshal([]byte(s), &decoded); err != nil {
			return nil, fmt.Errorf("pagination_parameters: not valid JSON: %v", err)
		}
		raw = decoded
	}
	list, ok := raw.([]interface{})
	if !ok {
		if raw == nil || raw == "" {
			return nil, fmt.Errorf("pagination_parameters: add at least one {type, name, value} for pagination_type update_parameter")
		}
		return nil, fmt.Errorf("pagination_parameters: want a list of {type, name, value}, got %T", raw)
	}
	var out []paginationParam
	for i, el := range list {
		row, ok := el.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("pagination_parameters[%d]: want an object, got %T", i, el)
		}
		p := paginationParam{in: strings.ToLower(stringifyValue(row["type"])), name: stringifyValue(row["name"]), value: stringifyValue(row["value"])}
		switch p.in {
		case "", "qs", "query":
			p.in = "query"
		case "headers", "header":
			p.in = "header"
		case "body":
		default:
			return nil, fmt.Errorf("pagination_parameters[%d].type: unknown value %q (use query, header or body)", i, p.in)
		}
		if strings.TrimSpace(p.name) == "" {
			return nil, fmt.Errorf("pagination_parameters[%d].name is empty", i)
		}
		if strings.TrimSpace(p.value) == "" {
			return nil, fmt.Errorf("pagination_parameters[%d].value is empty: without it every request is the same", i)
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pagination_parameters: add at least one {type, name, value}")
	}
	return out, nil
}

var (
	responseVarPattern  = regexp.MustCompile(`\$response\b`)
	pageCountVarPattern = regexp.MustCompile(`\$pageCount\b`)
)

// evalPageTemplate evaluates a pagination template. $response (body,
// headers with lower-case names, statusCode) is the previous response and
// $pageCount the number of pages fetched so far; $json is the input item.
func (r *requestRun) evalPageTemplate(tmpl string, item workflow.Item, prev *fetched, pageCount int) (string, error) {
	tmpl = responseVarPattern.ReplaceAllString(tmpl, "$$json.__response")
	tmpl = pageCountVarPattern.ReplaceAllString(tmpl, "$$json.__page_count")
	data := make(map[string]interface{}, len(item.JSON)+2)
	for k, v := range item.JSON {
		data[k] = v
	}
	resp := map[string]interface{}{"body": map[string]interface{}{}, "headers": map[string]interface{}{}, "statusCode": 0.0, "status": 0.0}
	if prev != nil {
		resp = map[string]interface{}{
			"body":       decodeBody(prev.body, "json"),
			"headers":    headerMap(prev.header, true),
			"statusCode": float64(prev.status),
			"status":     float64(prev.status),
		}
	}
	data["__response"] = resp
	data["__page_count"] = float64(pageCount)
	return r.engine.EvaluateString(tmpl, workflow.ExpressionContext{
		JSON: data, Node: r.input.NodeOutputs, WorkflowID: r.input.WorkflowID, ExecutionID: r.input.ExecutionID,
	})
}

// responseIsEmpty: no body, or JSON null, "", [] or {}.
func responseIsEmpty(f *fetched) bool {
	s := strings.TrimSpace(string(f.body))
	switch s {
	case "", "null", `""`, "[]", "{}":
		return true
	}
	var v interface{}
	if json.Unmarshal(f.body, &v) == nil {
		switch t := v.(type) {
		case []interface{}:
			return len(t) == 0
		case map[string]interface{}:
			return len(t) == 0
		}
	}
	return false
}

// paginate runs the update_parameter and next_url modes for one item.
func (r *requestRun) paginate(ctx context.Context, itemConfig map[string]interface{}, item workflow.Item, auth authPlan) (mainItems, errItems []workflow.Item) {
	fail := func(err error) ([]workflow.Item, []workflow.Item) {
		return mainItems, append(errItems, errorItem(err))
	}
	var params []paginationParam
	if r.pag.mode == "update_parameter" {
		var err error
		if params, err = parsePaginationParams(itemConfig["pagination_parameters"]); err != nil {
			return fail(fmt.Errorf("http.request: %w", err))
		}
	}
	nextTmpl := configString(itemConfig, "pagination_next_url")
	completeTmpl := configString(itemConfig, "pagination_complete_expression")

	var prev *fetched
	var current string
	for page := 0; page < r.pag.maxPages; page++ {
		if page > 0 && r.pag.interval > 0 {
			if err := sleepCtx(ctx, r.pag.interval); err != nil {
				return fail(err)
			}
		}
		ov := pageOverrides{}
		for _, p := range params {
			v, err := r.evalPageTemplate(p.value, item, prev, page)
			if err != nil {
				return fail(fmt.Errorf("http.request: pagination_parameters %s: %w", p.name, err))
			}
			ov.set(p.in, p.name, v)
		}
		if r.pag.mode == "next_url" && page > 0 {
			next, err := r.evalPageTemplate(nextTmpl, item, prev, page)
			if err != nil {
				return fail(fmt.Errorf("http.request: pagination_next_url: %w", err))
			}
			if next = strings.TrimSpace(next); next == "" {
				break
			}
			ov.url, ov.base = next, current
		}

		ob, err := r.prepare(ctx, itemConfig, item, auth, ov, -1)
		if err != nil {
			return fail(err)
		}
		f, err := send(ctx, r.client, ob, r.copts.maxBodyBytes, itemConfig, item.JSON)
		if err != nil {
			return fail(err)
		}
		current = f.url.String()

		if r.pag.completeWhen == "status_codes" && r.pag.statusCodes[f.status] {
			break
		}
		if r.pag.completeWhen == "response_is_empty" && isSuccess(f.status) && responseIsEmpty(f) {
			break
		}
		if prev != nil && prev.status == f.status && bytes.Equal(prev.body, f.body) {
			// The server ignored the page parameter: stop rather than loop.
			break
		}
		items, failed := shapeResponse(f, r.ropts)
		if failed {
			return mainItems, append(errItems, items...)
		}
		mainItems = append(mainItems, items...)
		if r.pag.completeWhen == "expression" {
			out, err := r.evalPageTemplate(completeTmpl, item, f, page+1)
			if err != nil {
				return fail(fmt.Errorf("http.request: pagination_complete_expression: %w", err))
			}
			if done, _ := strconv.ParseBool(strings.TrimSpace(out)); done {
				break
			}
		}
		prev = f
	}
	return mainItems, errItems
}

// paginateOffset is the original offset mode, unchanged in behaviour.
func (r *requestRun) paginateOffset(ctx context.Context, itemConfig map[string]interface{}, item workflow.Item, auth authPlan) (mainItems, errItems []workflow.Item) {
	for page := 0; page < r.pag.maxPages; page++ {
		ob, err := r.prepare(ctx, itemConfig, item, auth, pageOverrides{}, page)
		if err != nil {
			return mainItems, append(errItems, errorItem(err))
		}
		f, err := send(ctx, r.client, ob, r.copts.maxBodyBytes, itemConfig, item.JSON)
		if err != nil {
			return mainItems, append(errItems, errorItem(err))
		}
		items, failed := shapeResponse(f, responseOptions{format: r.ropts.format, fullResponse: true, neverError: r.ropts.neverError, outputProperty: r.ropts.outputProperty, lowercaseHeaders: r.ropts.lowercaseHeaders})
		if failed {
			return mainItems, append(errItems, items...)
		}
		list, ok := items[0].JSON["body"].([]interface{})
		if !ok {
			return append(mainItems, items...), errItems
		}
		if len(list) == 0 {
			break
		}
		for _, el := range list {
			m, ok := el.(map[string]interface{})
			if !ok {
				m = map[string]interface{}{"value": el}
			}
			mainItems = append(mainItems, workflow.NewItem(m))
		}
		if len(list) < r.pag.pageSize {
			break
		}
	}
	return mainItems, errItems
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
