package vector

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/monoes/mono-agent/internal/workflow"
)

// Embedding providers accepted by vector.embed and vector.store's nested
// "embedding" block.
const (
	ProviderOpenAICompatible = "openai_compatible"
	ProviderAzureOpenAI      = "azure_openai"
	ProviderOllama           = "ollama"
	ProviderCohere           = "cohere"
	ProviderGemini           = "gemini"
	ProviderMistral          = "mistral"
	ProviderHuggingFace      = "huggingface"
	ProviderBedrock          = "bedrock"
	ProviderDatabricks       = "databricks"
	ProviderVertex           = "vertex"
	ProviderOracle           = "oracle"
)

// EmbeddingProviders lists every provider in display order.
var EmbeddingProviders = []string{
	ProviderOpenAICompatible, ProviderOllama, ProviderCohere, ProviderGemini,
	ProviderMistral, ProviderHuggingFace, ProviderBedrock, ProviderAzureOpenAI,
	ProviderDatabricks, ProviderVertex, ProviderOracle,
}

// purpose tells providers that distinguish them (Cohere, Gemini) whether a
// text is a document being stored or a query being searched with.
type purpose string

const (
	purposeDocument purpose = "document"
	purposeQuery    purpose = "query"
)

// embedConfig is the parsed embedding configuration.
type embedConfig struct {
	Provider   string
	Model      string
	BaseURL    string
	APIKey     string
	BatchSize  int
	Dimensions int
	InputType  string // Cohere input_type override

	// azure_openai
	ResourceName string
	APIVersion   string
	// bedrock
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
	// databricks (also access_token) and vertex
	AccessToken  string
	ClientID     string
	ClientSecret string
	// vertex
	ProjectID   string
	Location    string
	ClientEmail string
	PrivateKey  string
	// oracle
	ConnectionString string
}

// parseEmbedConfig validates an embedding block. field names the config
// path in error messages ("" for vector.embed's top level, "embedding." for
// vector.store's nested block).
func parseEmbedConfig(cfg map[string]interface{}, field string) (embedConfig, error) {
	c := embedConfig{
		Provider:         strings.ToLower(strOr(cfg, ProviderOpenAICompatible, "provider")),
		Model:            str(cfg, "model"),
		BaseURL:          trimSlash(strOr(cfg, "", "base_url", "url", "endpoint", "host")),
		APIKey:           str(cfg, "api_key"),
		InputType:        str(cfg, "input_type"),
		ResourceName:     str(cfg, "resource_name"),
		APIVersion:       str(cfg, "api_version"),
		Region:           str(cfg, "region"),
		AccessKeyID:      str(cfg, "access_key_id"),
		SecretAccessKey:  str(cfg, "secret_access_key"),
		SessionToken:     str(cfg, "session_token"),
		AccessToken:      str(cfg, "access_token"),
		ClientID:         str(cfg, "client_id"),
		ClientSecret:     str(cfg, "client_secret"),
		ProjectID:        str(cfg, "project_id"),
		Location:         strOr(cfg, "", "location", "region"),
		ClientEmail:      strOr(cfg, "", "client_email", "email"),
		PrivateKey:       str(cfg, "private_key"),
		ConnectionString: str(cfg, "connection_string"),
	}
	var err error
	if c.BatchSize, err = intField(cfg, "batch_size", 100); err != nil {
		return c, fmt.Errorf("%s%w", field, err)
	}
	if c.BatchSize == 0 {
		c.BatchSize = 100
	}
	if _, set := cfg["batch_size"]; !set && c.Provider == ProviderVertex {
		c.BatchSize = 5 // Vertex caps the instances (and tokens) of one predict call
	}
	if c.Dimensions, err = intField(cfg, "dimensions", 0); err != nil {
		return c, fmt.Errorf("%s%w", field, err)
	}
	// A region or location becomes part of the provider's host
	// (bedrock-runtime.<region>.amazonaws.com, <location>-aiplatform…):
	// only a name, so it can't name a host of its own.
	for k, v := range map[string]string{"region": c.Region, "location": c.Location} {
		if v != "" && !regionName.MatchString(v) {
			return c, fmt.Errorf("%s%s %q is not a region name (letters, digits and dashes)", field, k, v)
		}
	}
	switch c.Provider {
	case ProviderOpenAICompatible:
		c.BaseURL = orDefault(c.BaseURL, "https://api.openai.com/v1")
		c.Model = orDefault(c.Model, "text-embedding-3-small")
	case ProviderMistral:
		c.BaseURL = orDefault(c.BaseURL, "https://api.mistral.ai/v1")
		c.Model = orDefault(c.Model, "mistral-embed")
	case ProviderOllama:
		c.BaseURL = orDefault(c.BaseURL, "http://localhost:11434")
		c.Model = orDefault(c.Model, "nomic-embed-text")
	case ProviderCohere:
		c.BaseURL = orDefault(c.BaseURL, "https://api.cohere.com")
		c.Model = orDefault(c.Model, "embed-english-v3.0")
	case ProviderGemini:
		c.BaseURL = orDefault(c.BaseURL, "https://generativelanguage.googleapis.com")
		c.Model = strings.TrimPrefix(orDefault(c.Model, "text-embedding-004"), "models/")
	case ProviderHuggingFace:
		c.BaseURL = orDefault(c.BaseURL, "https://router.huggingface.co/hf-inference")
		c.Model = orDefault(c.Model, "sentence-transformers/all-MiniLM-L6-v2")
	case ProviderBedrock:
		c.Region = orDefault(c.Region, "us-east-1")
		c.BaseURL = orDefault(c.BaseURL, "https://bedrock-runtime."+c.Region+".amazonaws.com")
		c.Model = orDefault(c.Model, "amazon.titan-embed-text-v2:0")
		if c.AccessKeyID == "" || c.SecretAccessKey == "" {
			return c, fmt.Errorf("%saccess_key_id and %ssecret_access_key are required for provider bedrock (use an \"aws\" connection)", field, field)
		}
	case ProviderAzureOpenAI:
		if c.BaseURL == "" && c.ResourceName == "" {
			return c, fmt.Errorf("%sresource_name or %sendpoint is required for provider azure_openai", field, field)
		}
		if c.BaseURL == "" {
			c.BaseURL = "https://" + c.ResourceName + ".openai.azure.com"
		}
		c.APIVersion = orDefault(c.APIVersion, "2024-10-21")
		if c.Model == "" {
			return c, fmt.Errorf("%smodel (the Azure deployment name) is required for provider azure_openai", field)
		}
	case ProviderDatabricks, ProviderVertex, ProviderOracle:
		if err := c.parseMore(field); err != nil {
			return c, err
		}
	default:
		return c, fmt.Errorf("%sprovider %q is not supported (want one of %s)", field, c.Provider, strings.Join(EmbeddingProviders, ", "))
	}
	switch c.Provider {
	case ProviderOpenAICompatible, ProviderOllama, ProviderBedrock, ProviderDatabricks, ProviderVertex, ProviderOracle:
		// Keys are optional: local OpenAI-compatible servers and Ollama run
		// without one; Bedrock signs with AWS keys checked above, and
		// parseMore checked the others' credentials.
	default:
		if c.APIKey == "" {
			return c, fmt.Errorf("%sapi_key is required for provider %s (set credential_id to a saved connection)", field, c.Provider)
		}
	}
	return c, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// embed returns one vector per text, batching requests by BatchSize.
func (c embedConfig) embed(ctx context.Context, texts []string, p purpose) ([][]float64, error) {
	if err := c.authorize(ctx); err != nil {
		return nil, fmt.Errorf("%s embeddings: %w", c.Provider, err)
	}
	out := make([][]float64, 0, len(texts))
	for start := 0; start < len(texts); start += c.BatchSize {
		end := start + c.BatchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]
		vecs, err := c.embedBatch(ctx, batch, p)
		if err != nil {
			return nil, fmt.Errorf("%s embeddings (model %s): %w", c.Provider, c.Model, err)
		}
		if len(vecs) != len(batch) {
			return nil, fmt.Errorf("%s embeddings: asked for %d vectors, got %d", c.Provider, len(batch), len(vecs))
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (c embedConfig) embedBatch(ctx context.Context, texts []string, p purpose) ([][]float64, error) {
	switch c.Provider {
	case ProviderOpenAICompatible, ProviderMistral:
		return c.embedOpenAI(ctx, c.BaseURL+"/embeddings", map[string]string{"Authorization": bearer(c.APIKey)}, texts)
	case ProviderAzureOpenAI:
		url := fmt.Sprintf("%s/openai/deployments/%s/embeddings?api-version=%s", c.BaseURL, c.Model, c.APIVersion)
		return c.embedOpenAI(ctx, url, map[string]string{"api-key": c.APIKey}, texts)
	case ProviderOllama:
		return c.embedOllama(ctx, texts)
	case ProviderCohere:
		return c.embedCohere(ctx, texts, p)
	case ProviderGemini:
		return c.embedGemini(ctx, texts, p)
	case ProviderHuggingFace:
		return c.embedHuggingFace(ctx, texts)
	case ProviderBedrock:
		return c.embedBedrock(ctx, texts, p)
	case ProviderDatabricks:
		return c.embedOpenAI(ctx, c.BaseURL+"/ai-gateway/openai/v1/embeddings", map[string]string{"Authorization": bearer(c.AccessToken)}, texts)
	case ProviderVertex:
		return c.embedVertex(ctx, texts, p)
	case ProviderOracle:
		return c.embedOracle(ctx, texts)
	}
	return nil, fmt.Errorf("unsupported provider %q", c.Provider)
}

func bearer(key string) string {
	if key == "" {
		return ""
	}
	return "Bearer " + key
}

// ── vector.embed ────────────────────────────────────────────────────────────

// EmbedNode implements vector.embed: one embedding vector per input item.
type EmbedNode struct{}

func (n *EmbedNode) Type() string { return "vector.embed" }

// regionName is a cloud region or location ("eu-west-1", "global").
var regionName = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// embedDestinations are where an embedding provider's key goes: its base
// URL, and the Azure resource its host is built from (a region or location
// is a name within the provider's own domain, parseEmbedConfig).
var embedDestinations = [][]string{{"base_url", "url", "endpoint", "host"}, {"resource_name"}}

// CredentialDestinationKeys: embedDestinations
// (workflow.CheckCredentialDestinations).
func (n *EmbedNode) CredentialDestinationKeys() [][]string { return embedDestinations }

func (n *EmbedNode) Execute(ctx context.Context, input workflow.NodeInput, config map[string]interface{}) ([]workflow.NodeOutput, error) {
	ec, err := parseEmbedConfig(config, "")
	if err != nil {
		return nil, fmt.Errorf("vector.embed: %w", err)
	}
	outKey := strOr(config, "embedding", "output_key")
	fields := stringList(config, "input_fields")
	if f := str(config, "input_field"); f != "" {
		fields = append([]string{f}, fields...)
	}

	texts := make([]string, len(input.Items))
	for i, item := range input.Items {
		t, err := itemText(item.JSON, fields)
		if err != nil {
			return nil, fmt.Errorf("vector.embed: item %d: %w", i, err)
		}
		texts[i] = t
	}
	if len(texts) == 0 {
		return []workflow.NodeOutput{{Handle: "main", Items: nil}}, nil
	}
	p := purposeDocument
	if strings.EqualFold(str(config, "purpose"), "query") {
		p = purposeQuery
	}
	vecs, err := ec.embed(ctx, texts, p)
	if err != nil {
		return nil, fmt.Errorf("vector.embed: %w", err)
	}
	out := make([]workflow.Item, len(input.Items))
	for i, item := range input.Items {
		j := copyMap(item.JSON)
		j[outKey] = vecs[i]
		out[i] = workflow.Item{JSON: j, Binary: item.Binary}
	}
	return []workflow.NodeOutput{{Handle: "main", Items: out}}, nil
}

// defaultTextFields are tried in order when no input field is configured:
// vector.load_document / vector.split_text emit pageContent.
var defaultTextFields = []string{"pageContent", "text", "content"}

// itemText extracts the text to embed. With fields set, their values are
// joined by a blank line; otherwise the first default field present wins,
// and as a last resort the whole item is embedded as JSON.
func itemText(j map[string]interface{}, fields []string) (string, error) {
	if len(fields) > 0 {
		parts := make([]string, 0, len(fields))
		for _, f := range fields {
			v, ok := lookupPath(j, f)
			if !ok {
				return "", fmt.Errorf("input field %q is missing", f)
			}
			if s := valueText(v); s != "" {
				parts = append(parts, s)
			}
		}
		t := strings.Join(parts, "\n\n")
		if strings.TrimSpace(t) == "" {
			return "", fmt.Errorf("input field(s) %s are empty", strings.Join(fields, ", "))
		}
		return t, nil
	}
	for _, f := range defaultTextFields {
		if v, ok := j[f]; ok {
			if s := valueText(v); strings.TrimSpace(s) != "" {
				return s, nil
			}
		}
	}
	if len(j) == 0 {
		return "", fmt.Errorf("item is empty; nothing to embed (set input_field)")
	}
	b, _ := json.Marshal(j)
	return string(b), nil
}

func valueText(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64, bool, int:
		return fmt.Sprint(t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
