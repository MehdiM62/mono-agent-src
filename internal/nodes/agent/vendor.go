package agent

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Model vendors agent.ask reaches with the user's own key instead of a
// signed-in agent CLI: the turn runs on opencode against the vendor's
// OpenAI-compatible endpoint, configured for this turn only.
//
// The connection (credential_id) brings its platform and fields; the key
// travels in the environment ({env:…} in the config), never in argv or a
// file.

// vendorProvider is the opencode provider id the turn's config defines.
const vendorProvider = "monoagent_vendor"

// vendorKeyEnv is the environment variable the provider reads its key from.
const vendorKeyEnv = "MONOAGENT_VENDOR_API_KEY"

type vendorSpec struct {
	name         string
	keyField     string // credential field holding the key ("" = none needed)
	defaultModel string // "" = the model must be set
	baseURL      func(cred map[string]interface{}) (string, error)
}

func credStr(cred map[string]interface{}, key string) string {
	if v, ok := cred[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func trimURL(u string) string { return strings.TrimRight(u, "/") }

// vendors maps a connection platform to the vendor's OpenAI-compatible API.
var vendors = map[string]vendorSpec{
	"gemini_api": {name: "Google Gemini", keyField: "api_key", defaultModel: "gemini-2.5-flash",
		baseURL: func(c map[string]interface{}) (string, error) {
			host := credStr(c, "host")
			if host == "" {
				host = "https://generativelanguage.googleapis.com"
			}
			return trimURL(host) + "/v1beta/openai", nil
		}},
	"ollama": {name: "Ollama", keyField: "api_key",
		baseURL: func(c map[string]interface{}) (string, error) {
			u := credStr(c, "base_url")
			if u == "" {
				u = "http://localhost:11434"
			}
			return trimURL(u) + "/v1", nil
		}},
	"moonshot": {name: "Moonshot (Kimi)", keyField: "api_key", defaultModel: "kimi-k2-0905-preview",
		baseURL: func(c map[string]interface{}) (string, error) {
			if strings.EqualFold(credStr(c, "region"), "china") {
				return "https://api.moonshot.cn/v1", nil
			}
			return "https://api.moonshot.ai/v1", nil
		}},
	"minimax": {name: "MiniMax", keyField: "api_key", defaultModel: "MiniMax-M2",
		baseURL: func(c map[string]interface{}) (string, error) {
			if strings.EqualFold(credStr(c, "region"), "china") {
				return "https://api.minimaxi.com/v1", nil
			}
			return "https://api.minimax.io/v1", nil
		}},
	"alibaba_cloud": {name: "Alibaba Cloud Model Studio", keyField: "api_key", defaultModel: "qwen-plus",
		baseURL: func(c map[string]interface{}) (string, error) {
			region := credStr(c, "region")
			if region == "" {
				region = "ap-southeast-1"
			}
			hosts := map[string]string{
				"ap-southeast-1": "https://dashscope-intl.aliyuncs.com",
				"us-east-1":      "https://dashscope-us.aliyuncs.com",
				"cn-beijing":     "https://dashscope.aliyuncs.com",
				"cn-hongkong":    "https://cn-hongkong.dashscope.aliyuncs.com",
			}
			if region == "eu-central-1" {
				ws := credStr(c, "workspace_id")
				if ws == "" {
					return "", fmt.Errorf("the eu-central-1 region needs the connection's workspace_id")
				}
				return "https://" + ws + ".eu-central-1.maas.aliyuncs.com/compatible-mode/v1", nil
			}
			h, ok := hosts[region]
			if !ok {
				return "", fmt.Errorf("unknown Alibaba Cloud region %q", region)
			}
			return h + "/compatible-mode/v1", nil
		}},
	"databricks": {name: "Databricks model serving", keyField: "access_token",
		baseURL: func(c map[string]interface{}) (string, error) {
			host := credStr(c, "host")
			if host == "" {
				return "", fmt.Errorf("the Databricks connection has no host")
			}
			if !strings.Contains(host, "://") {
				host = "https://" + host
			}
			return trimURL(host) + "/serving-endpoints", nil
		}},
}

// VendorPlatforms lists the connection platforms agent.ask can use as a
// model vendor (for help texts).
func VendorPlatforms() []string {
	return []string{"gemini_api", "ollama", "moonshot", "minimax", "alibaba_cloud", "databricks"}
}

// vendorTurn is how a turn reaches a vendor: the runtime and model to pass,
// and the environment that configures opencode for it.
type vendorTurn struct {
	runtime, model string
	env            map[string]string
}

// vendorFor reads the vendor connection of a config (credential_platform
// and credential, injected by the engine from credential_id). It returns
// nil when the config has no vendor connection.
func vendorFor(config map[string]interface{}, model string) (*vendorTurn, error) {
	platform, _ := config["credential_platform"].(string)
	spec, ok := vendors[platform]
	if !ok {
		return nil, nil
	}
	cred, _ := config["credential"].(map[string]interface{})
	base, err := spec.baseURL(cred)
	if err != nil {
		return nil, fmt.Errorf("agent.ask: %s: %w", spec.name, err)
	}
	model = strings.TrimPrefix(strings.TrimSpace(model), "models/")
	if model == "" {
		model = spec.defaultModel
	}
	if model == "" {
		return nil, fmt.Errorf("agent.ask: %s: set model (the model or serving endpoint name)", spec.name)
	}
	key := credStr(cred, spec.keyField)
	if key == "" && platform != "ollama" {
		return nil, fmt.Errorf("agent.ask: the %s connection has no %s", spec.name, spec.keyField)
	}
	if key == "" {
		key = "ollama" // the OpenAI-compatible client wants a key; Ollama ignores it
	}
	conf := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]interface{}{
			vendorProvider: map[string]interface{}{
				"npm":     "@ai-sdk/openai-compatible",
				"name":    spec.name,
				"options": map[string]interface{}{"baseURL": base, "apiKey": "{env:" + vendorKeyEnv + "}"},
				"models":  map[string]interface{}{model: map[string]interface{}{"name": model}},
			},
		},
	}
	raw, err := json.Marshal(conf)
	if err != nil {
		return nil, err
	}
	return &vendorTurn{
		runtime: "opencode",
		model:   vendorProvider + "/" + model,
		env:     map[string]string{"OPENCODE_CONFIG_CONTENT": string(raw), vendorKeyEnv: key},
	}, nil
}
