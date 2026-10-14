package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/workflow"
)

func TestVendorEndpoints(t *testing.T) {
	cases := []struct {
		platform string
		cred     map[string]interface{}
		model    string
		wantBase string
		wantID   string
	}{
		{"gemini_api", map[string]interface{}{"api_key": "sk-test-gemini"}, "models/gemini-2.5-pro", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.5-pro"},
		{"gemini_api", map[string]interface{}{"api_key": "sk-test-gemini"}, "", "https://generativelanguage.googleapis.com/v1beta/openai", "gemini-2.5-flash"},
		{"ollama", map[string]interface{}{"base_url": "http://gpu:11434/"}, "llama3.2", "http://gpu:11434/v1", "llama3.2"},
		{"moonshot", map[string]interface{}{"api_key": "sk-test-moon", "region": "china"}, "", "https://api.moonshot.cn/v1", "kimi-k2-0905-preview"},
		{"minimax", map[string]interface{}{"api_key": "sk-test-minimax"}, "", "https://api.minimax.io/v1", "MiniMax-M2"},
		{"alibaba_cloud", map[string]interface{}{"api_key": "sk-test-ali", "region": "us-east-1"}, "", "https://dashscope-us.aliyuncs.com/compatible-mode/v1", "qwen-plus"},
		{"alibaba_cloud", map[string]interface{}{"api_key": "sk-test-ali", "region": "eu-central-1", "workspace_id": "ws1"}, "qwen-max", "https://ws1.eu-central-1.maas.aliyuncs.com/compatible-mode/v1", "qwen-max"},
		{"databricks", map[string]interface{}{"host": "dbc-1.cloud.databricks.com", "access_token": "dapi"}, "my-endpoint", "https://dbc-1.cloud.databricks.com/serving-endpoints", "my-endpoint"},
	}
	for _, c := range cases {
		v, err := vendorFor(map[string]interface{}{"credential_platform": c.platform, "credential": c.cred}, c.model)
		if err != nil || v == nil {
			t.Fatalf("%s: %v", c.platform, err)
		}
		if v.runtime != "opencode" || v.model != vendorProvider+"/"+c.wantID {
			t.Errorf("%s: runtime %s model %s", c.platform, v.runtime, v.model)
		}
		var conf struct {
			Provider map[string]struct {
				Options map[string]string          `json:"options"`
				Models  map[string]json.RawMessage `json:"models"`
			} `json:"provider"`
		}
		if err := json.Unmarshal([]byte(v.env["OPENCODE_CONFIG_CONTENT"]), &conf); err != nil {
			t.Fatal(err)
		}
		p := conf.Provider[vendorProvider]
		if p.Options["baseURL"] != c.wantBase || p.Options["apiKey"] != "{env:"+vendorKeyEnv+"}" || p.Models[c.wantID] == nil {
			t.Errorf("%s: provider %+v", c.platform, p)
		}
		if v.env[vendorKeyEnv] == "" {
			t.Errorf("%s: no key in the environment", c.platform)
		}
		if key, _ := c.cred["api_key"].(string); key != "" && strings.Contains(v.env["OPENCODE_CONFIG_CONTENT"], key) {
			t.Errorf("%s: the key is in the config", c.platform)
		}
	}
	for name, cfg := range map[string]map[string]interface{}{
		"no key":          {"credential_platform": "moonshot", "credential": map[string]interface{}{}},
		"eu no workspace": {"credential_platform": "alibaba_cloud", "credential": map[string]interface{}{"api_key": "sk-test-ali", "region": "eu-central-1"}},
		"bad region":      {"credential_platform": "alibaba_cloud", "credential": map[string]interface{}{"api_key": "sk-test-ali", "region": "mars-1"}},
		"no host":         {"credential_platform": "databricks", "credential": map[string]interface{}{"access_token": "d"}},
		"no endpoint":     {"credential_platform": "databricks", "credential": map[string]interface{}{"host": "h", "access_token": "d"}},
	} {
		if _, err := vendorFor(cfg, ""); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if v, err := vendorFor(map[string]interface{}{"credential_platform": "slack"}, ""); v != nil || err != nil {
		t.Errorf("other platforms are not vendors: %v %v", v, err)
	}
}

// agent.ask with a vendor connection runs on opencode with the turn's
// provider config in the environment, also when an ai.model sub-node carries it.
func TestAskUsesVendorConnection(t *testing.T) {
	var got monomind.ExecOptions
	fakeTurns(t, func(o monomind.ExecOptions) (*monomind.TurnResult, error) {
		got = o
		return &monomind.TurnResult{ResultText: "ok"}, nil
	})
	cfg := map[string]interface{}{"prompt": "hi", "credential_id": "c1", "credential_platform": "gemini_api",
		"credential": map[string]interface{}{"api_key": "secret-g"}}
	if _, err := (&AskNode{}).Execute(context.Background(), workflow.NodeInput{Items: items(1)}, cfg); err != nil {
		t.Fatal(err)
	}
	if got.Runtime != "opencode" || got.Model != vendorProvider+"/gemini-2.5-flash" || got.Env[vendorKeyEnv] != "secret-g" {
		t.Fatalf("turn: runtime %q model %q env %v", got.Runtime, got.Model, got.Env)
	}

	in := workflow.NodeInput{Items: items(1), SubNodes: []workflow.SubNode{{Name: "Kimi", Type: "ai.model", Kind: "ai_languageModel",
		Config: map[string]interface{}{"model": "kimi-k2", "credential_platform": "moonshot", "credential": map[string]interface{}{"api_key": "secret-m"}}}}}
	if _, err := (&AskNode{}).Execute(context.Background(), in, map[string]interface{}{"prompt": "hi"}); err != nil {
		t.Fatal(err)
	}
	if got.Runtime != "opencode" || got.Model != vendorProvider+"/kimi-k2" || got.Env[vendorKeyEnv] != "secret-m" ||
		!strings.Contains(got.Env["OPENCODE_CONFIG_CONTENT"], "api.moonshot.ai") {
		t.Fatalf("sub-node: runtime %q model %q env %v", got.Runtime, got.Model, got.Env)
	}
}
