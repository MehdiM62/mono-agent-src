package apipkg

// Key is "<resource>.<operation>", or just the operation when the package
// has no resources.
func (o *Operation) Key() string {
	if o.Resource == "" {
		return o.Operation
	}
	return o.Resource + "." + o.Operation
}

// Pagination types.
const (
	PageCursor = "cursor"
	// PageHeaderCursor: the cursor comes from a response header
	// (cursor_path = header name) and goes back as a request header
	// (cursor_param = header name): Cosmos DB x-ms-continuation.
	PageHeaderCursor = "header_cursor"
	PageOffset       = "offset"
	PagePage         = "page"
	PageLinkHeader   = "link_header"
	PageNextURL      = "next_url"
)

// Pagination describes how a list operation pages.
type Pagination struct {
	Type        string `json:"type"`
	CursorParam string `json:"cursor_param,omitempty"`
	CursorPath  string `json:"cursor_path,omitempty"`
	HasMorePath string `json:"has_more_path,omitempty"`
	OffsetParam string `json:"offset_param,omitempty"`
	LimitParam  string `json:"limit_param,omitempty"`
	PageParam   string `json:"page_param,omitempty"`
	NextURLPath string `json:"next_url_path,omitempty"`
	// NextURLPrefix (a template) is prepended to a relative next URL whose
	// base is not the request host (Confluence v2 _links.next).
	NextURLPrefix string `json:"next_url_prefix,omitempty"`
	// Values: the page's results are an object keyed by id (Mautic
	// {"contacts": {"12": {…}}}); its values are the list (numeric keys in
	// numeric order, else sorted).
	Values bool `json:"values,omitempty"`
	// When (a template) turns paging off when it renders false: one plain
	// request, no page params (Strapi v3 vs v4 by credential). The param
	// names (cursor/offset/limit/page_param) may be templates too.
	When           string `json:"when,omitempty"`
	ResultsPath    string `json:"results_path,omitempty"`
	ReturnAllField string `json:"return_all_field,omitempty"`
	LimitField     string `json:"limit_field,omitempty"`

	// PageSize is the page size requested with limit_param (default 100).
	PageSize int `json:"page_size,omitempty"`
	// StartPage is the first page number for type "page" (default 1).
	StartPage *int `json:"start_page,omitempty"`
	// ParamIn is where page params go: "query" (default) or "body" (a JSON
	// body, e.g. search endpoints that take the cursor in the body).
	ParamIn string `json:"param_in,omitempty"`
	// MaxPages caps one run (default 1000); items get
	// pagination_truncated=true when it stops a list early.
	MaxPages int `json:"max_pages,omitempty"`
	// Next sends every page after the first as this request instead of
	// the op's (continuation endpoints: Dropbox list_folder/continue with
	// only the cursor; Elasticsearch search_after with the new PIT id).
	// res is the previous page and page.cursor its cursor.
	Next *Step `json:"next,omitempty"`
}

// Output says which part of the response becomes the node's items.
type Output struct {
	Path  string `json:"path,omitempty"`
	Split *bool  `json:"split,omitempty"`
	// HeaderRow turns a list of value rows into objects keyed by the
	// first row (Google Sheets values, CSV-like APIs).
	HeaderRow bool `json:"header_row,omitempty"`
	// Each replaces every result element by its value at this dot path
	// (Chargebee lists return [{"invoice": {…}}]; n8n emits the invoice).
	Each string `json:"each,omitempty"`
	// Match keeps only the result elements that satisfy n8n-style filter
	// rows (Google Sheets read lookups).
	Match *OutputMatch `json:"match,omitempty"`
	// Expand sends one request per result element (el = the element) and
	// replaces the element with the decoded response (Gmail lists ids,
	// then GETs each message).
	Expand *Step `json:"expand,omitempty"`
	// ExpandMerge: with expand, set element[key] to the response's value at
	// path ("" = the whole response) instead of replacing the element
	// (Gong call transcripts).
	ExpandMergeKey  string `json:"expand_merge_key,omitempty"`
	ExpandMergePath string `json:"expand_merge_path,omitempty"`
	// From (a template over res, or a builder such as $map) replaces the
	// decoded response before path/split: "{{ res | values }}" for maps
	// keyed by id; {"$map": "{{ res.data.urls }}", "$to": {"url": "{{ el }}"}}
	// turns a list of strings into objects.
	From any `json:"from,omitempty"`
	// Merge folds a list of objects into one object (Hue update results).
	Merge bool `json:"merge,omitempty"`
	// Skip drops the first n elements (NextCloud lists the folder itself
	// first); Sort orders elements by a dot path (SortDesc: descending).
	Skip     int    `json:"skip,omitempty"`
	Sort     string `json:"sort,omitempty"`
	SortDesc bool   `json:"sort_desc,omitempty"`
	// Limit applies n8n's returnAll/limit fields to a list that isn't
	// paginated (client-side slicing).
	Limit bool `json:"limit,omitempty"`
	// To reshapes each result element with a template (el = the element),
	// e.g. {"id": "{{ el.id }}", "$merge": "{{ el.values }}"}. ToWhen, a
	// template, skips the reshape when it renders empty/false (rawData).
	To     any    `json:"to,omitempty"`
	ToWhen string `json:"to_when,omitempty"`
	// MatchAfterTo runs Match on the elements reshaped by To instead of
	// the raw ones (Excel rows zipped with their column names, then
	// filtered by column).
	MatchAfterTo bool `json:"match_after_to,omitempty"`
	// Firestore (a template): when true, decode Firestore documents
	// ({name, fields{typed values}}) into plain JSON with _name, _id,
	// _createTime and _updateTime (n8n's "simple" output).
	Firestore string `json:"firestore,omitempty"`
	// Download fetches files per element onto the item's binary data.
	Download *DownloadStep `json:"download,omitempty"`
	// Where (a template, el = the element) drops the elements it renders
	// empty or false for (Graph delta: only entries with a file facet).
	Where string `json:"where,omitempty"`
	// Walk appends the records of a tree walk (a request repeated over a
	// growing frontier of ids) to the main result: Confluence descendants.
	Walk *Walk `json:"walk,omitempty"`
	// SchemaRows is a path (in the whole response) to a BigQuery-style
	// schema ([{name, type, mode, fields}]); the rows at output.path
	// ({f: [{v}]}) become objects keyed by field name. Numbers (a
	// template) casts INTEGER/NUMERIC/FLOAT values to numbers when true.
	SchemaRows string `json:"schema_rows,omitempty"`
	Numbers    string `json:"numbers,omitempty"`
}

// OutputMatch filters result elements by condition rows. Rows is a
// template resolving to a list of objects; each row's Column/Value keys
// name the element field and the wanted value. Combine ("AND"/"OR",
// default AND) and First (keep only the first match) are templates.
type OutputMatch struct {
	Rows    string `json:"rows"`
	Column  string `json:"column"`
	Value   string `json:"value"`
	Combine string `json:"combine,omitempty"`
	First   string `json:"first,omitempty"`
}

// Binary configures file upload and download.
type Binary struct {
	// Download: the response body is a file, stored on the output item's
	// binary data.
	Download bool `json:"download,omitempty"`
	// UploadField is the op param naming the input item's binary property
	// to send (n8n's "binaryPropertyName").
	UploadField string `json:"upload_field,omitempty"`
	// PropertyField is the op param naming the output binary property for a
	// download (default property "data"). UploadField and PropertyField may
	// be dotted ("options.binaryPropertyOutput") to read a collection key.
	PropertyField string `json:"property_field,omitempty"`
	// MultipartField is the form field of the file part (default "file").
	MultipartField string `json:"multipart_field,omitempty"`
	// UploadWhen (a template) decides per run whether to upload at all
	// (n8n sendBinaryData); when false the op sends its body (JSON, or
	// none). Property (a template) overrides the binary property read
	// from upload_field ("formField:binaryProperty" values).
	UploadWhen string `json:"upload_when,omitempty"`
	// FallbackBodyType is the body type when upload_when turns the upload
	// off (default: json when the op has a body, else none).
	FallbackBodyType string `json:"fallback_body_type,omitempty"`
	Property         string `json:"property,omitempty"`
	// Properties (a template or builder giving a list, or comma text)
	// uploads several binary properties: one multipart part each (el =
	// {property, index} in multipart_field), and bin.files lists them for
	// JSON bodies. A $map builder picks them from a collection
	// ({"$map": "{{ p.refs.values }}", "$to": "{{ el.inputType | eq:'binary' | then:el.binaryPropertyName }}"}).
	Properties any `json:"properties,omitempty"`
	// FileName and MimeType are templates for the uploaded part.
	FileName string `json:"file_name,omitempty"`
	MimeType string `json:"mime_type,omitempty"`
	// DownloadWhen (a template) decides per run whether the response is a
	// file (GCS alt=media, Slides thumbnail download); it overrides Download.
	DownloadWhen string `json:"download_when,omitempty"`
	// UploadText (a template): when it renders non-empty, its text is the
	// upload instead of the input item's binary (GCS create from text).
	UploadText string `json:"upload_text,omitempty"`
	// UploadURL (a template): when it renders a URL, the file at that URL
	// is fetched (GET, without the credential) and uploaded in place of
	// the input item's binary (Airtop's "upload from URL" source).
	UploadURL string `json:"upload_url,omitempty"`
	// UploadURLs (a template or builder giving a list, or comma text)
	// fetches every http(s) URL (GET, without the credential) and adds
	// each file to bin.files after binary.properties' own (property
	// "url_<i>"): media given by URL that the API wants inline (Gemini,
	// Ollama image analysis).
	UploadURLs any `json:"upload_urls,omitempty"`
	// UploadSession sends files larger than MinSize in chunks through a
	// resumable upload session (Microsoft Graph createUploadSession)
	// instead of one raw request.
	UploadSession *UploadSession `json:"upload_session,omitempty"`
	// DownloadBase64 (a template over res): when the response carries the
	// file base64-encoded in JSON (GitHub contents), decode it onto the
	// item's binary and keep the JSON response as the item's json.
	DownloadBase64 string `json:"download_base64,omitempty"`
	// DownloadEach is a response path (may be a template over p) to a list
	// of base64 files: each element (el in download_base64, file_name,
	// mime_type) becomes one item (OpenAI image generations with n > 1).
	DownloadEach string `json:"download_each,omitempty"`
}
