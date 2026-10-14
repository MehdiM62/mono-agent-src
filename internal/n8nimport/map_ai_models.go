package n8nimport

import (
	"fmt"
	"strings"
)

// modelRoute is the local agent runtime that runs an n8n chat model
// sub-node's models. prefix is prepended to the model name (opencode names
// models "<provider>/<model>"); note says what the user must set up; gap,
// when set, is why the mapping is only partial.
type modelRoute struct {
	runtime, prefix, note, gap string
}

// modelRoutes maps n8n chat model sub-node types to agent.ask runtimes
// (`monoagentcli agent scan` ids). Vendors without a runtime of their own
// go through opencode, which serves many providers by "<provider>/<model>".
var modelRoutes = map[string]modelRoute{
	lc + "lmChatOpenAi": {runtime: "codex"},
	lc + "lmOpenAi":     {runtime: "codex"},
	lc + "lmChatAzureOpenAi": {runtime: "codex", gap: "codex runs OpenAI models with its own sign-in, not the Azure deployment: " +
		"check the model (n8n gave the deployment name)"},
	lc + "lmChatAnthropic":    {runtime: "claude"},
	lc + "lmChatGoogleGemini": {runtime: "antigravity"},
	lc + "lmChatGoogleVertex": {runtime: "antigravity", note: "Gemini runs with the antigravity sign-in, not the Vertex project"},
	lc + "lmChatXAiGrok":      {runtime: "grok"},
	lc + "lmChatMoonshot":     {runtime: "kimicode"},
	lc + "lmChatAlibabaCloud": {runtime: "qwen"},
	lc + "lmChatDeepSeek":     {runtime: "dsh"},
	lc + "lmChatOpenRouter":   {runtime: "opencode", prefix: "openrouter/", note: "opencode needs an OpenRouter key (opencode auth login)"},
	lc + "lmChatGroq":         {runtime: "opencode", prefix: "groq/", note: "opencode needs a Groq key (opencode auth login)"},
	lc + "lmChatMistralCloud": {runtime: "opencode", prefix: "mistral/", note: "opencode needs a Mistral key (opencode auth login)"},
	lc + "lmChatVercelAiGateway": {runtime: "opencode", prefix: "vercel/",
		note: "opencode needs a Vercel AI Gateway key (opencode auth login)"},
	lc + "lmChatNvidia":     {runtime: "opencode", prefix: "nvidia/", note: "opencode needs an NVIDIA key (opencode auth login)"},
	lc + "lmChatAwsBedrock": {runtime: "opencode", prefix: "amazon-bedrock/", note: "opencode reads the AWS credentials of the environment"},
	lc + "lmChatMinimax":    {runtime: "opencode", prefix: "minimax/", note: "opencode needs a MiniMax key (opencode auth login)"},
	lc + "lmOpenHuggingFaceInference": {runtime: "opencode", prefix: "huggingface/",
		note: "opencode needs a Hugging Face token (opencode auth login)"},
	lc + "lmChatOllama": {runtime: "opencode", prefix: "ollama/", note: "opencode needs the Ollama server as a provider in its opencode.json"},
	lc + "lmOllama":     {runtime: "opencode", prefix: "ollama/", note: "opencode needs the Ollama server as a provider in its opencode.json"},
	lc + "lmChatCohere": {runtime: "claude", gap: "no local agent runtime runs Cohere models; the claude runtime answers instead"},
	lc + "lmCohere":     {runtime: "claude", gap: "no local agent runtime runs Cohere models; the claude runtime answers instead"},
	lc + "lmChatDatabricks": {runtime: "claude", gap: "no local agent runtime calls Databricks model serving; " +
		"the claude runtime answers instead"},
	lc + "lmChatLemonade": {runtime: "claude", gap: "no local agent runtime talks to a Lemonade server; the claude runtime answers instead"},
	lc + "lmLemonade":     {runtime: "claude", gap: "no local agent runtime talks to a Lemonade server; the claude runtime answers instead"},
}

// modelName reads the model a sub-node selects (plain string or locator).
func modelName(params map[string]interface{}) string {
	for _, k := range []string{"model", "modelName", "modelId"} {
		switch v := params[k].(type) {
		case string:
			return strings.TrimPrefix(v, "models/")
		case map[string]interface{}:
			return strings.TrimPrefix(fmt.Sprint(v["value"]), "models/")
		}
	}
	return ""
}

// applyModel keeps the attached chat model, memory and output parser as
// ai.model / ai.memory / ai.output_parser sub-nodes of the node whose
// config is cfg, and reports the sub-nodes agent.ask cannot use. Without a
// chat model cfg runs on the claude runtime.
func applyModel(c *MapContext, cfg map[string]interface{}) {
	found := false
	for _, s := range c.SubNodes {
		switch s.Kind {
		case "ai_languageModel":
			if found {
				c.Warn("fallback model", s.Name, "agent.ask has no fallback model")
				continue
			}
			found = true
			attachModel(c, cfg, s)
		case "ai_tool":
			c.Warn("tools", s.Name, "the %s tool is kept as the disabled node %q: agent.ask cannot call tools yet, so the agent answers without it",
				shortType(s.Type), s.Name)
		case "ai_memory":
			if mem, ok := chatMemoryConfig(c, s); ok {
				c.attach(s.Name, s.Kind, "ai.memory", mem, cfg)
			} else {
				c.Warn("memory", s.Name, "conversation memory (%s) is not kept between runs", shortType(s.Type))
			}
		case "ai_outputParser":
			attachOutputParser(c, cfg, s)
		default:
			c.Warn(s.Kind, s.Name, "sub-node %s is not supported", shortType(s.Type))
		}
	}
	if !found {
		cfg["runtime"] = "claude"
		c.Note("no chat model is attached; the claude runtime is used")
	}
}

// attachModel keeps chat model s as an ai.model sub-node of cfg's node.
func attachModel(c *MapContext, cfg map[string]interface{}, s SubNode) {
	model := map[string]interface{}{"runtime": "claude"}
	useModel(c, model, s)
	c.attach(s.Name, s.Kind, "ai.model", model, cfg)
}

// useModel sets runtime and model (ai.model's config) from one chat model
// sub-node. A Model Selector contributes its first model.
func useModel(c *MapContext, cfg map[string]interface{}, s SubNode) {
	if s.Type == lc+"modelSelector" {
		for _, m := range s.Children {
			if m.Kind == "ai_languageModel" {
				c.Warn("model selector", s.Name, "the rules of %s are not applied: model %q (%s) always answers",
					shortType(s.Type), m.Name, shortType(m.Type))
				useModel(c, cfg, m)
				return
			}
		}
		c.Warn("model selector", s.Name, "%s has no chat model attached; the claude runtime is used", shortType(s.Type))
		return
	}
	model := modelName(s.Parameters)
	if useVendorAccount(c, cfg, s, model) {
		return
	}
	route, ok := modelRoutes[s.Type]
	if !ok {
		c.Warn("model", s.Name, "model %q (%s) has no local agent runtime; the claude runtime is used — set runtime/model to choose another",
			model, shortType(s.Type))
		return
	}
	cfg["runtime"] = route.runtime
	if route.gap != "" {
		c.Warn("model", s.Name, "model %q (%s): %s", model, shortType(s.Type), route.gap)
		if route.runtime != "claude" && model != "" {
			cfg["model"] = route.prefix + model
		}
		return
	}
	if model != "" {
		cfg["model"] = route.prefix + model
	}
	msg := fmt.Sprintf("model %q (%s) runs through the local %s agent", route.prefix+model, shortType(s.Type), route.runtime)
	if route.note != "" {
		msg += "; " + route.note
	}
	c.Note("%s", msg)
}

// vendorAccounts are the chat models agent.ask runs with the user's own
// vendor key: the n8n credential types and the mono connection platform
// (agent.ask's Model Vendor Account).
var vendorAccounts = map[string]struct {
	platform string
	n8nTypes []string
}{
	lc + "lmChatGoogleGemini": {"gemini_api", []string{"googlePalmApi"}},
	lc + "lmChatOllama":       {"ollama", []string{"ollamaApi"}},
	lc + "lmOllama":           {"ollama", []string{"ollamaApi"}},
	lc + "lmChatMoonshot":     {"moonshot", []string{"moonshotApi"}},
	lc + "lmChatMinimax":      {"minimax", []string{"minimaxApi"}},
	lc + "lmChatAlibabaCloud": {"alibaba_cloud", []string{"alibabaCloudApi"}},
	lc + "lmChatDatabricks":   {"databricks", []string{"databricksOAuth2Api", "databricksApi"}},
}

// useVendorAccount runs chat model s through its vendor connection when the
// n8n node carries the vendor's credential: credential_id names the
// connection, which supplies the runtime.
func useVendorAccount(c *MapContext, cfg map[string]interface{}, s SubNode, model string) bool {
	v, ok := vendorAccounts[s.Type]
	if !ok {
		return false
	}
	use := credUse{Platform: v.platform}
	for _, t := range v.n8nTypes {
		if cred, ok := s.Credentials[t]; ok {
			use.N8nType, use.N8nName = t, cred.Name
			break
		}
	}
	if use.N8nType == "" {
		return false
	}
	c.creds = append(c.creds, use)
	delete(cfg, "runtime")
	cfg["credential_id"] = v.platform
	if model != "" {
		cfg["model"] = model
	}
	c.Note("model %q (%s) runs with the key of the %s connection", model, shortType(s.Type), v.platform)
	if use.N8nType == "databricksOAuth2Api" {
		c.Note("the databricks connection needs the workspace host and a personal access token (access_token)")
	}
	return true
}
