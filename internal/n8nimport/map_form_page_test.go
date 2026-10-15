package n8nimport_test

import (
	"encoding/json"
	"testing"
)

// n8n's Form node imports as core.form: a page keeps its fields in n8n's
// shape (answers keyed as n8n keyed them) and reports what it cannot show.
func TestFormPageImports(t *testing.T) {
	wf, rep := convertNode(t, "n8n-nodes-base.form", 2.4, map[string]interface{}{
		"formFields": map[string]interface{}{"values": []interface{}{
			map[string]interface{}{"fieldLabel": "Your age", "fieldName": "age", "fieldType": "number", "requiredField": true},
			map[string]interface{}{"fieldLabel": "Size", "fieldType": "radio",
				"fieldOptions": map[string]interface{}{"values": []interface{}{map[string]interface{}{"option": "S"}}}},
			map[string]interface{}{"fieldLabel": "CV", "fieldType": "file"},
		}},
		"limitWaitTime": true, "resumeAmount": 2.0, "resumeUnit": "days",
		"options": map[string]interface{}{"formTitle": "Step 2", "buttonLabel": "Next", "customCss": "x{}"},
	})
	page := node(t, wf, "Trigger")
	if page.Type != "core.form" {
		t.Fatalf("type %s", page.Type)
	}
	wantConfig(t, page, map[string]interface{}{"operation": "page", "formTitle": "Step 2", "buttonLabel": "Next",
		"limitWaitTime": true, "limitType": "afterTimeInterval", "resumeAmount": 2.0, "resumeUnit": "days"})
	raw, _ := json.Marshal(page.Config["formFields"])
	want := `[{"fieldLabel":"Your age","fieldName":"age","fieldType":"number","requiredField":true},` +
		`{"fieldLabel":"Size","fieldOptions":{"values":[{"option":"S"}]},"fieldType":"radio"}]`
	if string(raw) != want {
		t.Errorf("formFields %s", raw)
	}
	wantIssue(t, rep, "file upload fields are not supported")
	wantIssue(t, rep, "custom CSS is not applied")
}

func TestFormEndingImports(t *testing.T) {
	wf, _ := convertNode(t, "n8n-nodes-base.form", 2.5, map[string]interface{}{
		"operation": "completion", "completionTitle": "Thanks", "completionMessage": "We got it."})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"operation": "completion", "respondWith": "text",
		"completionTitle": "Thanks", "completionMessage": "We got it."})

	wf, _ = convertNode(t, "n8n-nodes-base.form", 2.5, map[string]interface{}{
		"operation": "completion", "respondWith": "redirect", "redirectUrl": "https://example.com"})
	wantConfig(t, node(t, wf, "Trigger"), map[string]interface{}{"respondWith": "redirect", "redirectUrl": "https://example.com"})

	_, rep := convertNode(t, "n8n-nodes-base.form", 2.5, map[string]interface{}{
		"operation": "completion", "respondWith": "returnBinary", "inputDataFieldName": "data"})
	wantIssue(t, rep, "cannot send a file")
}
