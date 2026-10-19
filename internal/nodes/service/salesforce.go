package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/apipkg"
	"github.com/monoes/mono-agent/internal/workflow"
)

// SalesforceNode implements the service.salesforce node type.
type SalesforceNode struct{}

func (n *SalesforceNode) Type() string { return "service.salesforce" }

// CredentialDestinationKeys: the org the access token goes to
// (workflow.CheckCredentialDestinations).
func (n *SalesforceNode) CredentialDestinationKeys() [][]string {
	return [][]string{{"instance_url"}}
}

// salesforceDefaultAPIVersion is the REST API version used when the node's
// api_version field is empty (66.0 = Spring '26). Salesforce keeps old
// versions available for years, so saved workflows keep working when this
// moves; set api_version to pin one.
const salesforceDefaultAPIVersion = "66.0"

// salesforceAPIVersionRe matches "66.0" or "v66.0".
var salesforceAPIVersionRe = regexp.MustCompile(`^v?(\d{1,3}\.\d{1,2})$`)

// salesforceAPIVersion returns the path segment ("v66.0") for the configured
// api_version, or the default when unset.
func salesforceAPIVersion(config map[string]interface{}) (string, error) {
	v := strings.TrimSpace(strVal(config, "api_version"))
	if v == "" {
		v = salesforceDefaultAPIVersion
	}
	m := salesforceAPIVersionRe.FindStringSubmatch(v)
	if m == nil {
		return "", fmt.Errorf("salesforce: api_version %q is invalid: use a number like %s (or v%s)", v, salesforceDefaultAPIVersion, salesforceDefaultAPIVersion)
	}
	return "v" + m[1], nil
}

func (n *SalesforceNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	if extensionOwns("service.salesforce", config) {
		return apipkg.ExecuteExtension(ctx, "service.salesforce", input, config)
	}
	instanceURL := strVal(config, "instance_url")
	if instanceURL == "" {
		return nil, fmt.Errorf("salesforce: instance_url is required")
	}
	accessToken := strVal(config, "access_token")
	if accessToken == "" {
		return nil, fmt.Errorf("salesforce: access_token is required")
	}
	apiVersion, err := salesforceAPIVersion(config)
	if err != nil {
		return nil, err
	}
	operation := strVal(config, "operation")
	baseURL := strings.TrimRight(instanceURL, "/") + "/services/data/" + apiVersion

	var items []workflow.Item

	switch operation {
	case "query":
		soql := strVal(config, "soql")
		if soql == "" {
			return nil, fmt.Errorf("salesforce: soql is required for query")
		}
		records, err := salesforceQuery(ctx, baseURL, accessToken, soql)
		if err != nil {
			return nil, err
		}
		items = records

	case "get_record":
		objectType := strVal(config, "object_type")
		recordID := strVal(config, "record_id")
		if objectType == "" || recordID == "" {
			return nil, fmt.Errorf("salesforce: object_type and record_id are required for get_record")
		}
		u := baseURL + "/sobjects/" + url.PathEscape(objectType) + "/" + url.PathEscape(recordID)
		data, err := apiRequest(ctx, "GET", u, accessToken, nil)
		if err != nil {
			return nil, err
		}
		items = []workflow.Item{workflow.NewItem(data)}

	case "create_record":
		objectType := strVal(config, "object_type")
		if objectType == "" {
			return nil, fmt.Errorf("salesforce: object_type is required for create_record")
		}
		fields := mapVal(config, "fields")
		if fields == nil {
			fields = map[string]interface{}{}
		}
		u := baseURL + "/sobjects/" + url.PathEscape(objectType)
		data, err := apiRequest(ctx, "POST", u, accessToken, fields)
		if err != nil {
			return nil, err
		}
		items = []workflow.Item{workflow.NewItem(data)}

	case "update_record":
		objectType := strVal(config, "object_type")
		recordID := strVal(config, "record_id")
		if objectType == "" || recordID == "" {
			return nil, fmt.Errorf("salesforce: object_type and record_id are required for update_record")
		}
		fields := mapVal(config, "fields")
		if fields == nil {
			fields = map[string]interface{}{}
		}
		u := baseURL + "/sobjects/" + url.PathEscape(objectType) + "/" + url.PathEscape(recordID)
		_, err := apiRequest(ctx, "PATCH", u, accessToken, fields)
		if err != nil {
			return nil, err
		}
		// PATCH returns 204 no content; return confirmation item
		items = []workflow.Item{workflow.NewItem(map[string]interface{}{
			"id":      recordID,
			"updated": true,
		})}

	case "delete_record":
		objectType := strVal(config, "object_type")
		recordID := strVal(config, "record_id")
		if objectType == "" || recordID == "" {
			return nil, fmt.Errorf("salesforce: object_type and record_id are required for delete_record")
		}
		u := baseURL + "/sobjects/" + url.PathEscape(objectType) + "/" + url.PathEscape(recordID)
		_, err := apiRequest(ctx, "DELETE", u, accessToken, nil)
		if err != nil {
			return nil, err
		}
		items = []workflow.Item{workflow.NewItem(map[string]interface{}{
			"id":      recordID,
			"deleted": true,
		})}

	case "describe_object":
		objectType := strVal(config, "object_type")
		if objectType == "" {
			return nil, fmt.Errorf("salesforce: object_type is required for describe_object")
		}
		u := baseURL + "/sobjects/" + url.PathEscape(objectType) + "/describe"
		data, err := apiRequest(ctx, "GET", u, accessToken, nil)
		if err != nil {
			return nil, err
		}
		items = []workflow.Item{workflow.NewItem(data)}

	default:
		return nil, fmt.Errorf("salesforce: unknown operation %q", operation)
	}

	return []workflow.NodeOutput{{Handle: "main", Items: items}}, nil
}

// salesforceQuery executes a SOQL query and paginates through all records.
func salesforceQuery(ctx context.Context, baseURL, accessToken, soql string) ([]workflow.Item, error) {
	var allItems []workflow.Item

	// URL-encode the query
	queryURL := baseURL + "/query?" + url.Values{"q": {soql}}.Encode()

	for queryURL != "" {
		data, err := apiRequest(ctx, "GET", queryURL, accessToken, nil)
		if err != nil {
			return nil, fmt.Errorf("salesforce query: %w", err)
		}

		records, _ := data["records"].([]interface{})
		for _, r := range records {
			if m, ok := r.(map[string]interface{}); ok {
				allItems = append(allItems, workflow.NewItem(m))
			}
		}

		// Check for next page
		done, _ := data["done"].(bool)
		if done {
			break
		}
		nextURL, _ := data["nextRecordsUrl"].(string)
		if nextURL == "" {
			break
		}
		// nextRecordsUrl is a path like /services/data/v66.0/query/...
		// Extract the base instance URL
		// baseURL is like https://myorg.salesforce.com/services/data/v66.0
		// We need the instance root
		instanceRoot := salesforceInstanceRoot(baseURL)
		// Only its path counts: the response can't move the token to
		// another host ("@evil.example/…" pasted after the root would).
		root, err := url.Parse(instanceRoot + "/")
		if err != nil {
			return nil, fmt.Errorf("salesforce query: instance URL: %w", err)
		}
		next, err := url.Parse(nextURL)
		if err != nil || next.Scheme != "" || next.Host != "" || next.User != nil {
			return nil, fmt.Errorf("salesforce query: nextRecordsUrl %q is not a path on the instance", nextURL)
		}
		queryURL = root.ResolveReference(next).String()
	}

	return allItems, nil
}

// salesforceInstanceRoot extracts the instance root URL from a base URL.
// e.g. https://myorg.salesforce.com/services/data/v66.0 -> https://myorg.salesforce.com
func salesforceInstanceRoot(baseURL string) string {
	// Find the third slash (after https://)
	count := 0
	for i, c := range baseURL {
		if c == '/' {
			count++
			if count == 3 {
				return baseURL[:i]
			}
		}
	}
	return baseURL
}
