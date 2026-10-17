package apipkg

import (
	"fmt"
	"slices"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

func (o *Operation) check(where string, credKeys map[string]bool) []string {
	var probs []string
	add := func(format string, a ...any) { probs = append(probs, where+": "+fmt.Sprintf(format, a...)) }
	if !validMethods[strings.ToUpper(o.Method)] && !hasTemplate(o.Method) {
		add("method %q must be an HTTP/WebDAV method or a template", o.Method)
	}
	fields := map[string]bool{"resource": true, "operation": true}
	if o.Wait != nil {
		fields[hookFieldsMarker] = true // hook.url: the resume URL
	}
	for _, f := range o.Fields {
		if reservedFieldKeys[f.Key] {
			add("field key %q is reserved", f.Key)
		}
		if fields[f.Key] && !reservedFieldKeys[f.Key] {
			add("field %q is defined twice", f.Key)
		}
		fields[f.Key] = true
		if !allowedFieldTypes[f.Type] {
			add("field %q: unknown type %q", f.Key, f.Type)
		}
		if (f.Type == "select" || f.Type == "multiselect") && len(f.Options) == 0 {
			add("field %q: a %s needs options", f.Key, f.Type)
		}
	}
	if !strings.HasPrefix(o.Path, "/") && !strings.HasPrefix(o.Path, "{{") && !strings.HasPrefix(o.Path, "http") {
		add("path %q must start with / (or be an absolute URL)", o.Path)
	}
	probs = append(probs, checkTemplates(where+" base_url", o.BaseURL, fields, credKeys)...)
	probs = append(probs, checkTemplates(where+" path", o.Path, fields, credKeys)...)
	probs = append(probs, checkTemplates(where+" query", o.Query, fields, credKeys)...)
	probs = append(probs, checkTemplates(where+" body", o.Body, fields, credKeys)...)
	probs = append(probs, checkHeaderTemplates(where, o.Headers, fields, credKeys)...)
	probs = append(probs, checkTemplates(where+" error_when", []any{o.ErrorWhen, o.ErrorMessage}, fields, credKeys)...)
	if d := o.Output; d != nil && d.Download != nil {
		if d.Download.ForEach != "" {
			add("output.download: use files, not for_each")
		}
		if (!validMethods[strings.ToUpper(d.Download.Method)] && !hasTemplate(d.Download.Method)) || d.Download.Path == "" {
			add("output.download: needs a method (GET, …) and a path")
		}
		for _, v := range []any{d.Download.When, d.Download.Files, d.Download.BaseURL, d.Download.Path, d.Download.Query,
			d.Download.Body, d.Download.Property, d.Download.FileName, d.Download.MimeType} {
			probs = append(probs, checkTemplates(where+" output.download", v, fields, credKeys)...)
		}
		probs = append(probs, checkHeaderTemplates(where+" output.download", d.Download.Headers, fields, credKeys)...)
	}
	if o.Output != nil && o.Output.Expand != nil {
		st := o.Output.Expand
		for _, v := range []any{st.When, st.BaseURL, st.Path, st.Query, st.Body} {
			probs = append(probs, checkTemplates(where+" output.expand", v, fields, credKeys)...)
		}
	}
	if o.Output != nil {
		probs = append(probs, checkTemplates(where+" output.where", o.Output.Where, fields, credKeys)...)
	}
	if o.Output != nil && o.Output.Walk != nil {
		probs = append(probs, o.Output.Walk.check(where+" output.walk", fields, credKeys)...)
	}
	if pl := o.Poll; pl != nil {
		if !validMethods[pl.Method] || pl.Path == "" || pl.Until == "" {
			add("poll: needs method, path and until")
		}
		for _, v := range []any{pl.Until, pl.BaseURL, pl.Path, pl.Query, pl.Body} {
			probs = append(probs, checkTemplates(where+" poll", v, fields, credKeys)...)
		}
	}
	for i, st := range o.Before {
		if (!validMethods[strings.ToUpper(st.Method)] && !hasTemplate(st.Method)) || st.Path == "" {
			add("before[%d]: needs a method (GET, POST, …) and a path", i)
		}
		for _, v := range []any{st.When, st.BaseURL, st.Path, st.Query, st.Body} {
			probs = append(probs, checkTemplates(where+" before", v, fields, credKeys)...)
		}
	}
	for i, st := range o.Also {
		sw := fmt.Sprintf("%s also[%d]", where, i)
		if !validMethods[strings.ToUpper(st.Method)] && !hasTemplate(st.Method) {
			add("also[%d]: method %q must be an HTTP/WebDAV method or a template", i, st.Method)
		}
		if st.Path == "" {
			add("also[%d]: path is required", i)
		}
		for _, v := range []any{st.When, st.ForEach, st.BaseURL, st.Path, st.Query, st.Body} {
			probs = append(probs, checkTemplates(sw, v, fields, credKeys)...)
		}
		probs = append(probs, checkHeaderTemplates(sw, st.Headers, fields, credKeys)...)
	}
	bt := o.effectiveBodyType()
	if (bt == BodyForm || bt == BodyMultipart) && o.Body != nil {
		if _, ok := o.Body.(map[string]any); !ok {
			add("a %s body must be an object", bt)
		}
	}
	if bt == BodyNone && o.Body != nil {
		add(`body is set but body_type is "none"`)
	}
	if pg := o.Pagination; pg != nil {
		need := map[string][2]string{
			PageCursor:       {pg.CursorParam + "|" + pg.CursorPath, "cursor_param and cursor_path"},
			PageHeaderCursor: {pg.CursorParam + "|" + pg.CursorPath, "cursor_param and cursor_path (header names)"},
			PageOffset:       {pg.OffsetParam, "offset_param"},
			PagePage:         {pg.PageParam, "page_param"},
			PageNextURL:      {pg.NextURLPath, "next_url_path"},
		}
		if pg.Next != nil && pg.Type == PageCursor {
			// A continuation request carries the cursor in its own templates.
			need[PageCursor] = [2]string{pg.CursorPath, "cursor_path"}
		}
		if n, ok := need[pg.Type]; ok && (n[0] == "" || strings.HasPrefix(n[0], "|") || strings.HasSuffix(n[0], "|")) {
			add("pagination %q needs %s", pg.Type, n[1])
		}
		if st := pg.Next; st != nil {
			if pg.Type != PageCursor {
				add(`pagination.next works with type "cursor" only`)
			}
			probs = append(probs, checkStep(where+" pagination.next", *st, fields, credKeys)...)
		}
		if pg.ParamIn == "body" && bt != BodyJSON && bt != BodyForm && bt != BodyMultipart {
			add(`pagination param_in "body" needs a json, form or multipart body`)
		}
	}
	if b := o.Binary; b != nil {
		for _, f := range []string{b.UploadField, b.PropertyField} {
			// A dotted name reads a key of a collection field.
			if f != "" && !fields[strings.SplitN(f, ".", 2)[0]] {
				add("binary: %q is not one of the op's fields", f)
			}
		}
		probs = append(probs, checkTemplates(where+" binary", []any{b.FileName, b.MimeType}, fields, credKeys)...)
		if us := b.UploadSession; us != nil {
			if b.UploadField == "" {
				add("binary.upload_session needs binary.upload_field")
			}
			if !validMethods[strings.ToUpper(us.Create.Method)] || us.Create.Path == "" {
				add("binary.upload_session.create: needs a method (POST, …) and a path")
			}
			if us.MinSize < 0 || us.ChunkSize < 0 {
				add("binary.upload_session: min_size and chunk_size must not be negative")
			}
			for _, v := range []any{us.Create.When, us.Create.BaseURL, us.Create.Path, us.Create.Query, us.Create.Body} {
				probs = append(probs, checkTemplates(where+" binary.upload_session.create", v, fields, credKeys)...)
			}
			probs = append(probs, checkHeaderTemplates(where+" binary.upload_session.create", us.Create.Headers, fields, credKeys)...)
		}
	}
	if b := o.Bulk; b != nil {
		probs = append(probs, b.check(where+" bulk", fields, credKeys)...)
	}
	for i, st := range o.Before {
		sw := fmt.Sprintf("%s before[%d]", where, i)
		probs = append(probs, checkTemplates(sw, []any{st.ErrorWhen, st.ErrorMessage}, fields, credKeys)...)
		probs = append(probs, checkWaits(sw, st.Repeat, st.Stream, fields, credKeys)...)
	}
	for i, st := range o.Also {
		sw := fmt.Sprintf("%s also[%d]", where, i)
		probs = append(probs, checkTemplates(sw, []any{st.ErrorWhen, st.ErrorMessage}, fields, credKeys)...)
		probs = append(probs, checkWaits(sw, st.Repeat, st.Stream, fields, credKeys)...)
	}
	if pl := o.Poll; pl != nil {
		probs = append(probs, checkTemplates(where+" poll", []any{pl.ErrorWhen, pl.ErrorMessage, pl.MaxWaitSeconds}, fields, credKeys)...)
	}
	probs = append(probs, checkWaits(where, nil, o.Stream, fields, credKeys)...)
	if o.Stream != nil && (o.Pagination != nil || o.Bulk != nil || (o.Binary != nil && (o.Binary.Download || o.Binary.DownloadWhen != ""))) {
		add("stream does not combine with pagination, bulk or a binary download")
	}
	if b := o.Binary; b != nil {
		probs = append(probs, checkTemplates(where+" binary", []any{b.UploadURL, b.UploadURLs, b.DownloadEach, b.Properties}, fields, credKeys)...)
	}
	for n8n, ours := range o.N8nParamMap {
		if !fields[ours] {
			add("n8n_param_map: %q maps to %q, which is not one of the op's fields", n8n, ours)
		}
	}
	probs = append(probs, o.checkSends(where, fields)...)
	for k, sub := range o.Subfields {
		coll, opt, _ := strings.Cut(k, ".")
		if f := o.field(coll); f == nil || f.Type != "collection" && f.Type != "fixedCollection" || !slices.Contains(f.Options, opt) {
			add("subfields names %q, which is no option of a collection field", k)
		}
		if len(sub) == 0 {
			add("subfields %q lists no field", k)
		}
	}
	return probs
}

// field is o's field of key, or nil.
func (o *Operation) field(key string) *workflow.NodeSchemaField {
	for i := range o.Fields {
		if o.Fields[i].Key == key {
			return &o.Fields[i]
		}
	}
	return nil
}

// checkTemplates parses every template string in v and checks that p.X
// names a field (when fields is set) and cred.X a credential key.
var validMethods = map[string]bool{"GET": true, "POST": true, "PUT": true, "PATCH": true, "DELETE": true, "HEAD": true,
	// WebDAV (NextCloud, ownCloud)
	"MKCOL": true, "MOVE": true, "COPY": true, "PROPFIND": true, "PROPPATCH": true}

// checkWaits checks a step's repeat and a stream: until is required and
// every template must parse and name known fields.
func checkWaits(where string, rp *Repeat, st *Stream, fields, credKeys map[string]bool) []string {
	var probs []string
	if rp != nil {
		if rp.Until == "" {
			probs = append(probs, fmt.Sprintf("%s: repeat needs until", where))
		}
		if rp.IntervalMs < 0 || rp.MaxWaitMs < 0 {
			probs = append(probs, fmt.Sprintf("%s: repeat interval_ms and max_wait_ms must not be negative", where))
		}
		probs = append(probs, checkTemplates(where+" repeat", []any{rp.Until, rp.MaxWaitSeconds}, fields, credKeys)...)
	}
	if st != nil {
		if st.Until == "" {
			probs = append(probs, fmt.Sprintf("%s: stream needs until", where))
		}
		if st.TimeoutMs < 0 {
			probs = append(probs, fmt.Sprintf("%s: stream timeout_ms must not be negative", where))
		}
		probs = append(probs, checkTemplates(where+" stream", []any{st.Until, st.TimeoutSeconds}, fields, credKeys)...)
	}
	return probs
}
