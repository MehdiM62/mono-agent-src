package httpnodes

// RequestNodeSchema documents the config keys RequestNode.Execute reads out
// of its map[string]interface{} config — see
// internal/nodes/control.SetNodeSchema's doc comment for why this is a
// companion struct rather than the runtime config, and
// internal/tools/schemagen for the tag grammar (a literal comma or pipe in
// a tag value is written as the full-width '，' / '｜').
//
// Every key Execute reads is listed. credential_id picks a saved
// connection of any platform; the schema format has no way to say "any
// platform", so credential_platform stays null and the editor's picker
// falls back to its own platform lookup for this node type.
type RequestNodeSchema struct {
	CredentialID string `json:"credential_id" schema:"label=Saved connection,type=credential_picker,help=Use a saved connection's token or login. Its access_token or API key is sent as a Bearer token and a username and password as Basic auth unless Connection auth says otherwise. OAuth tokens are refreshed before the run."`

	URL string `json:"url" schema:"label=URL,type=text,required,placeholder=https://api.example.com/data,help=With a saved connection that has a base URL a path like /v1/items is enough."`

	Method string `json:"method" schema:"label=Method,type=select,required,options=GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS,default=GET"`

	AuthType string `json:"auth_type" schema:"label=Authentication,type=select,options=none|credential|basic|digest|bearer|api_key,default=none,help=credential uses the saved connection above (also the case with none when a connection is picked)."`

	CredentialAuth string `json:"credential_auth" schema:"label=Connection auth,type=text,placeholder=bearer,depends_on_key=auth_type,depends_on_values=none|credential,help=How to send the connection's secret: bearer (default for tokens and keys)， basic， header:X-API-Key or query:api_key. Leave empty to decide from the connection."`

	AuthUsername string `json:"auth_username" schema:"label=Username,type=text,depends_on_key=auth_type,depends_on_values=basic|digest"`

	AuthPassword string `json:"auth_password" schema:"label=Password,type=password,depends_on_key=auth_type,depends_on_values=basic|digest"`

	AuthToken string `json:"auth_token" schema:"label=Bearer token,type=password,depends_on_key=auth_type,depends_on_values=bearer,help=Sent as Authorization: Bearer <token>."`

	AuthAPIKeyIn string `json:"auth_api_key_in" schema:"label=API key location,type=select,options=header|query,default=header,depends_on_key=auth_type,depends_on_values=api_key"`

	AuthAPIKeyName string `json:"auth_api_key_name" schema:"label=API key name,type=text,placeholder=X-API-Key,depends_on_key=auth_type,depends_on_values=api_key,help=The header or query parameter name."`

	AuthAPIKeyValue string `json:"auth_api_key_value" schema:"label=API key value,type=password,depends_on_key=auth_type,depends_on_values=api_key"`

	QueryParams string `json:"query_params" schema:"label=Query parameters (JSON),type=textarea,rows=3,placeholder={ \"limit\": 50， \"tags\": [\"a\"， \"b\"] },help=An object or a list of {name， value}. A list value repeats the parameter (see Query array format)."`

	QueryArrayFormat string `json:"query_array_format" schema:"label=Query array format,type=select,options=repeat|brackets|indices,default=repeat,help=How a list value is written: repeat (a=1&a=2)， brackets (a[]=1) or indices (a[0]=1)."`

	Headers string `json:"headers" schema:"label=Headers (JSON),type=textarea,rows=3,placeholder={ \"Accept\": \"application/json\" },help=An object or a list of {name， value}."`

	BodyType string `json:"body_type" schema:"label=Body type,type=select,options=json|form|multipart|text|raw|binary|none,depends_on_key=method,depends_on_values=POST|PUT|PATCH,help=Left empty: an object or JSON text is sent as JSON and other text as text/plain. form is application/x-www-form-urlencoded; multipart is form-data with files; binary sends the input item's binary data."`

	Body string `json:"body" schema:"label=Request body,type=textarea,rows=5,depends_on_key=method,depends_on_values=POST|PUT|PATCH,help=JSON or text. For form and multipart an object of fields; a multipart file field is {\"file\": \"/path\"} or {\"binary_property\": \"data\"}."`

	RawContentType string `json:"raw_content_type" schema:"label=Raw content type,type=text,placeholder=application/xml,depends_on_key=body_type,depends_on_values=raw|binary,help=Content-Type for a raw or binary body (binary detects it when empty)."`

	BinaryProperty string `json:"binary_property" schema:"label=Binary property,type=text,default=data,depends_on_key=body_type,depends_on_values=binary,help=Which binary data of the input item to send."`

	ResponseFormat string `json:"response_format" schema:"label=Response format,type=select,options=json|text|autodetect|file|binary,default=json,help=json parses the body (text when it is not JSON); autodetect decides from Content-Type; file keeps the bytes as binary data on the item; binary returns base64 text."`

	FullResponse bool `json:"full_response" schema:"label=Include status and headers,type=boolean,default=true,help=On: each item is {status， headers， body}. Off: the body itself is the item (a list becomes one item per element)."`

	NeverError bool `json:"never_error" schema:"label=Never error,type=boolean,default=false,help=Send non-2xx responses to the main output instead of the error output."`

	OutputProperty string `json:"output_property" schema:"label=Output property,type=text,default=data,help=Where a text or binary body goes when status and headers are not included; also the binary data name for the file format."`

	LowercaseHeaders bool `json:"lowercase_headers" schema:"label=Lower-case response headers,type=boolean,default=false"`

	FollowRedirects bool `json:"follow_redirects" schema:"label=Follow redirects,type=boolean,default=true"`

	MaxRedirects float64 `json:"max_redirects" schema:"label=Max redirects,type=number,default=10,depends_on_key=follow_redirects,depends_on_values=true"`

	SendCredentialsOnRedirect bool `json:"send_credentials_on_redirect" schema:"label=Send credentials on cross-host redirects,type=boolean,default=false,help=Off (default): auth and custom headers are dropped when a redirect leaves the original host."`

	AllowUnauthorizedCerts bool `json:"allow_unauthorized_certs" schema:"label=Ignore TLS certificate errors,type=boolean,default=false"`

	Proxy string `json:"proxy" schema:"label=Proxy,type=text,placeholder=http://user:pass@proxy:8080"`

	Timeout float64 `json:"timeout" schema:"label=Timeout (seconds),type=number,default=30"`

	PaginationType string `json:"pagination_type" schema:"label=Pagination,type=select,options=none|offset|update_parameter|next_url,default=none,help=offset: page/per_page query parameters. update_parameter: set parameters per request from {{ $response }} and {{ $pageCount }}. next_url: follow a URL the response names."`

	PageField string `json:"page_field" schema:"label=Page parameter,type=text,default=page,depends_on_key=pagination_type,depends_on_values=offset"`

	PageSize float64 `json:"page_size" schema:"label=Page size,type=number,default=100,depends_on_key=pagination_type,depends_on_values=offset,help=Sent as per_page; a shorter page ends pagination."`

	PaginationParameters string `json:"pagination_parameters" schema:"label=Pagination parameters (JSON),type=textarea,rows=4,depends_on_key=pagination_type,depends_on_values=update_parameter,placeholder=[{ \"type\": \"query\"， \"name\": \"page\"， \"value\": \"{{ add $pageCount 1 }}\" }],help=A list of {type: query｜header｜body， name， value}. value is a template; $response is the previous response (body， headers， statusCode) and $pageCount the pages fetched so far."`

	PaginationNextURL string `json:"pagination_next_url" schema:"label=Next URL,type=text,placeholder={{ $response.body.next }},depends_on_key=pagination_type,depends_on_values=next_url,help=Template for the next page's URL from the previous response; empty ends pagination."`

	PaginationCompleteWhen string `json:"pagination_complete_when" schema:"label=Pagination complete when,type=select,options=response_is_empty|status_codes|expression,default=response_is_empty,depends_on_key=pagination_type,depends_on_values=update_parameter|next_url"`

	PaginationStatusCodes string `json:"pagination_status_codes" schema:"label=Complete on status codes,type=text,placeholder=404， 410,depends_on_key=pagination_complete_when,depends_on_values=status_codes"`

	PaginationCompleteExpression string `json:"pagination_complete_expression" schema:"label=Complete expression,type=text,placeholder={{ not $response.body.has_more }},depends_on_key=pagination_complete_when,depends_on_values=expression,help=Template evaluated on each response; true ends pagination after that page."`

	MaxPages float64 `json:"max_pages" schema:"label=Max pages,type=number,default=1000,depends_on_key=pagination_type,depends_on_values=offset|update_parameter|next_url"`

	PaginationIntervalMs float64 `json:"pagination_interval_ms" schema:"label=Delay between pages (ms),type=number,default=0,depends_on_key=pagination_type,depends_on_values=offset|update_parameter|next_url"`

	BatchSize float64 `json:"batch_size" schema:"label=Batch size,type=number,help=With a batch interval: pause after this many items."`

	BatchIntervalMs float64 `json:"batch_interval_ms" schema:"label=Batch interval (ms),type=number,help=Pause between batches of items."`

	PropagateTrace bool `json:"propagate_trace" schema:"label=Pass the org chain on to other hosts,type=boolean,default=false,help=Also send this run's signed chain (X-Monoagent-Trace) to other hosts: for a system that calls a webhook here back with it. When off only requests to this machine carry it."`

	MaxBodyMB float64 `json:"max_body_mb" schema:"label=Max Response Body (MB),type=number,default=64,help=Maximum response body size in megabytes (default 64). Larger responses fail with an error instead of exhausting memory."`
}
