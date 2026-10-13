package httpnodes

// N8nParam says where one n8n node parameter goes in our config.
type N8nParam struct {
	// Key is our config key ("" when the parameter has no equivalent and
	// is dropped, or only steers which other key is used).
	Key string
	// Values maps n8n option values onto ours, when they differ. A value
	// not listed is copied unchanged (the node also accepts the n8n
	// spellings listed here).
	Values map[string]string
	// Note says how to convert the value when copying is not enough.
	Note string
}

// N8nHTTPParamMap maps the n8n HTTP Request node (typeVersion 3–4.x)
// parameters, by their path in the node's "parameters" object, to
// http.request config keys. The importer uses it to convert an n8n
// workflow; the node accepts the n8n option spellings in Values too.
//
// n8n's key/value collections ({"parameters": [{"name", "value"}]}) can be
// passed as the list itself: http.request reads a list of {name, value}
// wherever it reads an object (query_params, headers, form bodies).
var N8nHTTPParamMap = map[string]N8nParam{
	"method": {Key: "method"},
	"url":    {Key: "url"},

	"authentication": {Key: "auth_type", Values: map[string]string{
		"none": "none", "predefinedCredentialType": "credential", "genericCredentialType": "",
	}, Note: "genericCredentialType: take auth_type from genericAuthType. predefinedCredentialType: set credential_id to the matching saved connection (nodeCredentialType names its platform)."},
	"genericAuthType": {Key: "auth_type", Values: map[string]string{
		"httpBasicAuth": "basic", "httpDigestAuth": "digest", "httpHeaderAuth": "header",
		"httpQueryAuth": "query", "httpBearerAuth": "bearer", "oAuth2Api": "credential", "oAuth1Api": "",
	}, Note: "httpHeaderAuth / httpQueryAuth: auth_api_key_name and auth_api_key_value from the credential's name and value. httpBasicAuth/httpDigestAuth: auth_username/auth_password from user/password. oAuth2Api: a saved OAuth connection as credential_id. oAuth1Api and httpCustomAuth have no equivalent."},
	"nodeCredentialType": {Key: "credential_id", Note: "the n8n credential type (e.g. githubApi) names the platform; pick the profile's connection for it. The node then derives the auth (credential_auth to override)."},

	"sendQuery":                  {Key: "", Note: "false: drop query_params."},
	"specifyQuery":               {Key: "", Note: "keypair: use queryParameters.parameters; json: use jsonQuery."},
	"queryParameters.parameters": {Key: "query_params", Note: "the [{name, value}] list as is."},
	"jsonQuery":                  {Key: "query_params", Note: "a JSON object string; copied as is."},

	"sendHeaders":                 {Key: "", Note: "false: drop headers."},
	"specifyHeaders":              {Key: "", Note: "keypair: use headerParameters.parameters; json: use jsonHeaders."},
	"headerParameters.parameters": {Key: "headers", Note: "the [{name, value}] list as is."},
	"jsonHeaders":                 {Key: "headers"},

	"sendBody": {Key: "", Note: "false: set body_type none."},
	"contentType": {Key: "body_type", Values: map[string]string{
		"json": "json", "form-urlencoded": "form", "multipart-form-data": "multipart", "binaryData": "binary", "raw": "raw",
	}},
	"specifyBody":               {Key: "", Note: "keypair: body from bodyParameters.parameters (an object of name: value for json; the list as is for form and multipart); json: body from jsonBody; string: body from body."},
	"bodyParameters.parameters": {Key: "body", Note: "multipart rows keep parameterType formBinaryData + inputDataFieldName, which the node reads."},
	"jsonBody":                  {Key: "body"},
	"body":                      {Key: "body"},
	"inputDataFieldName":        {Key: "binary_property"},
	"rawContentType":            {Key: "raw_content_type"},

	"options.batching.batch.batchSize":     {Key: "batch_size"},
	"options.batching.batch.batchInterval": {Key: "batch_interval_ms", Note: "milliseconds, as in n8n."},
	"options.allowUnauthorizedCerts":       {Key: "allow_unauthorized_certs"},
	"options.queryParameterArrays": {Key: "query_array_format", Values: map[string]string{
		"repeat": "repeat", "brackets": "brackets", "indices": "indices",
	}},
	"options.lowercaseHeaders":                  {Key: "lowercase_headers", Note: "n8n defaults to true; ours to false. Set it explicitly."},
	"options.redirect.redirect.followRedirects": {Key: "follow_redirects"},
	"options.redirect.redirect.maxRedirects":    {Key: "max_redirects"},
	"options.response.response.fullResponse":    {Key: "full_response", Note: "n8n defaults to false (body only); ours to true. Always set it: false when the n8n node did not include the full response."},
	"options.response.response.neverError":      {Key: "never_error"},
	"options.response.response.responseFormat": {Key: "response_format", Values: map[string]string{
		"autodetect": "autodetect", "file": "file", "json": "json", "text": "text",
	}, Note: "n8n defaults to autodetect; ours to json. Set it explicitly."},
	"options.response.response.outputPropertyName": {Key: "output_property"},
	"options.pagination.pagination.paginationMode": {Key: "pagination_type", Values: map[string]string{
		"off": "none", "updateAParameterInEachRequest": "update_parameter", "responseContainsNextURL": "next_url",
	}},
	"options.pagination.pagination.nextURL":               {Key: "pagination_next_url", Note: "an n8n expression: $response and $pageCount keep their names; JavaScript operators need our template functions ({{ $pageCount + 1 }} becomes {{ add $pageCount 1 }})."},
	"options.pagination.pagination.parameters.parameters": {Key: "pagination_parameters", Note: "rows of {type: qs|headers|body, name, value}; the node accepts n8n's type names."},
	"options.pagination.pagination.paginationCompleteWhen": {Key: "pagination_complete_when", Values: map[string]string{
		"responseIsEmpty": "response_is_empty", "receiveSpecificStatusCodes": "status_codes", "other": "expression",
	}},
	"options.pagination.pagination.statusCodesWhenComplete": {Key: "pagination_status_codes"},
	"options.pagination.pagination.completeExpression":      {Key: "pagination_complete_expression", Note: "an expression, converted like nextURL."},
	"options.pagination.pagination.limitPagesFetched":       {Key: "", Note: "true: copy maxRequests to max_pages. false: n8n has no cap; ours is 1000 pages unless max_pages is set."},
	"options.pagination.pagination.maxRequests":             {Key: "max_pages"},
	"options.pagination.pagination.requestInterval":         {Key: "pagination_interval_ms", Note: "milliseconds, as in n8n."},
	"options.proxy":   {Key: "proxy"},
	"options.timeout": {Key: "timeout", Note: "n8n is milliseconds; ours seconds — divide by 1000."},
	"options.sendCredentialsOnCrossOriginRedirect": {Key: "send_credentials_on_redirect"},

	"curlImport":                   {Key: "", Note: "UI-only."},
	"provideSslCertificates":       {Key: "", Note: "client certificates are not supported."},
	"options.webdavMethods":        {Key: "", Note: "any method name is accepted; nothing to set."},
	"infoMessage":                  {Key: ""},
	"googleApiWarning":             {Key: ""},
	"provideSslCertificatesNotice": {Key: ""},
}

// N8nGraphQLParamMap maps the n8n GraphQL node parameters to http.graphql
// config keys, like N8nHTTPParamMap.
var N8nGraphQLParamMap = map[string]N8nParam{
	"endpoint":      {Key: "endpoint"},
	"query":         {Key: "query"},
	"variables":     {Key: "variables"},
	"operationName": {Key: "operation_name"},
	"requestMethod": {Key: "request_method"},
	"requestFormat": {Key: "request_format"},
	"responseFormat": {Key: "response_format", Values: map[string]string{
		"json": "json", "string": "text",
	}},
	"dataPropertyName":             {Key: "data_property_name"},
	"allowUnauthorizedCerts":       {Key: "allow_unauthorized_certs"},
	"headerParametersUi.parameter": {Key: "headers", Note: "the [{name, value}] list as is."},
	"authentication": {Key: "auth_type", Values: map[string]string{
		"none": "none", "basicAuth": "basic", "digestAuth": "digest", "headerAuth": "header",
		"queryAuth": "query", "oAuth2": "credential", "predefinedCredentialType": "credential",
		"customAuth": "", "oAuth1": "",
	}, Note: "as for the HTTP Request node's genericAuthType."},
	"nodeCredentialType": {Key: "credential_id", Note: "as for the HTTP Request node."},
}
