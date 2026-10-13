package n8nimport

import (
	"strings"
	"testing"
)

func TestAppSourceDefaultsOperationParamAndTriggers(t *testing.T) {
	calls := 0
	RegisterAppSource(func() []AppMapper {
		calls++
		return []AppMapper{
			{N8nTypes: []string{"n8n-nodes-base.testFm"}, NodeType: "app.fm", PackageID: "fm",
				OperationParam: "action", DefaultResource: "record",
				DefaultOperations:  map[string]string{"record": "find", "layout": "list"},
				CredentialPlatform: map[string]string{"fmApi": "fm"},
				CredentialHints:    map[string]string{"fmApi": "monoagentcli connect fm; n8n credential fields: db → database"}},
			{N8nTypes: []string{"n8n-nodes-base.testFmTrigger"}, PackageID: "fm", Trigger: true},
		}
	})
	before := calls
	wf, rep := convertString(t, `{"name":"t","nodes":[
	 {"name":"M","type":"n8n-nodes-base.manualTrigger","typeVersion":1,"position":[0,0],"parameters":{}},
	 {"name":"Find","type":"n8n-nodes-base.testFm","typeVersion":1,"position":[200,0],"parameters":{"layout":"L"}},
	 {"name":"Create","type":"n8n-nodes-base.testFm","typeVersion":1,"position":[400,0],
	  "parameters":{"resource":"layout","action":"create","name":"x"},"credentials":{"fmApi":{"id":"1","name":"FM"}}},
	 {"name":"Layouts","type":"n8n-nodes-base.testFm","typeVersion":1,"position":[600,0],"parameters":{"resource":"layout"}},
	 {"name":"Trig","type":"n8n-nodes-base.testFmTrigger","typeVersion":1,"position":[0,200],"parameters":{}}],
	 "connections":{"M":{"main":[[{"node":"Find","type":"main","index":0}]]},
	  "Find":{"main":[[{"node":"Create","type":"main","index":0}]]},"Create":{"main":[[{"node":"Layouts","type":"main","index":0}]]}}}`)
	if calls == before {
		t.Error("Convert must read the app sources")
	}
	find := nodeByName(wf, "Find")
	if find.Type != "app.fm" || find.Config["resource"] != "record" || find.Config["action"] != "find" || find.Config["operation"] != nil || find.Config["layout"] != "L" {
		t.Errorf("default resource/operation: %v", find.Config)
	}
	if c := nodeByName(wf, "Create").Config; c["action"] != "create" || c["resource"] != "layout" || c["name"] != "x" {
		t.Errorf("operation param must be kept: %v", c)
	}
	if c := nodeByName(wf, "Layouts").Config; c["action"] != "list" {
		t.Errorf("per-resource default operation: %v", c)
	}
	if rep.Counts.Unmapped != 1 || !strings.Contains(rep.Text(), "n8n trigger n8n-nodes-base.testFmTrigger (API package fm) is not supported yet") {
		t.Errorf("trigger: %s", rep.Text())
	}
	for _, c := range rep.Credentials {
		if c.Platform != "fm" || !strings.Contains(c.Hint, "db → database") {
			t.Errorf("credential hint: %+v", c)
		}
	}
	_, app := SupportedTypes()
	if !contains(app, "n8n-nodes-base.testFm") || contains(app, "n8n-nodes-base.testFmTrigger") {
		t.Errorf("SupportedTypes: %v", app)
	}
}

func TestCredentialPlatformFor(t *testing.T) {
	for in, want := range map[string]string{
		"googleTranslateOAuth2Api": "google", "gmailOAuth2": "gmail", "githubApi": "github",
		"stripeApi": "stripe", "someServiceOAuth2Api": "someservice",
	} {
		if got := credentialPlatformFor(in); got != want {
			t.Errorf("credentialPlatformFor(%s) = %s, want %s", in, got, want)
		}
	}
}
