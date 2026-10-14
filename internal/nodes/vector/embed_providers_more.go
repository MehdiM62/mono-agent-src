package vector

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// googleTokenURL is where a Vertex service account trades its signed JWT
// for an access token; tests point it at a fake server.
var googleTokenURL = "https://oauth2.googleapis.com/token"

const googleCloudScope = "https://www.googleapis.com/auth/cloud-platform"

// parseMore fills in and checks the databricks, vertex and oracle settings.
func (c *embedConfig) parseMore(field string) error {
	switch c.Provider {
	case ProviderDatabricks:
		if c.BaseURL == "" {
			return fmt.Errorf("%shost is required for provider databricks (the workspace URL, e.g. https://adb-123.4.azuredatabricks.net)", field)
		}
		if c.Model == "" {
			return fmt.Errorf("%smodel (the serving endpoint name) is required for provider databricks", field)
		}
		if c.AccessToken == "" && (c.ClientID == "" || c.ClientSecret == "") {
			return fmt.Errorf("%saccess_token, or %sclient_id and %sclient_secret of a service principal, are required for provider databricks", field, field, field)
		}
	case ProviderVertex:
		c.Model = strings.TrimPrefix(orDefault(c.Model, "text-embedding-005"), "models/")
		c.Location = orDefault(c.Location, "us-central1")
		if c.ProjectID == "" {
			return fmt.Errorf("%sproject_id is required for provider vertex", field)
		}
		if c.BaseURL == "" {
			c.BaseURL = vertexBaseURL(c.Location)
		}
		if c.AccessToken == "" && (c.ClientEmail == "" || c.PrivateKey == "") {
			return fmt.Errorf("%saccess_token, or %sclient_email and %sprivate_key of a service account, are required for provider vertex", field, field, field)
		}
	case ProviderOracle:
		if c.ConnectionString == "" {
			return fmt.Errorf("%sconnection_string is required for provider oracle (use an oracle connection, e.g. oracle://user:pass@host:1521/service)", field)
		}
		if _, err := oracleIdent(field+"model", c.Model); err != nil {
			return fmt.Errorf("%w (the name of an ONNX model loaded into the database)", err)
		}
	}
	return nil
}

// vertexBaseURL is the Vertex AI host of a location: the global endpoint,
// a multi-region (eu, us) data-residency host, or the regional one.
func vertexBaseURL(location string) string {
	switch location {
	case "global":
		return "https://aiplatform.googleapis.com"
	case "eu", "us":
		return "https://aiplatform." + location + ".rep.googleapis.com"
	}
	return "https://" + location + "-aiplatform.googleapis.com"
}

// authorize fetches the access token of a databricks service principal or
// a vertex service account once per run, when no token is configured.
func (c *embedConfig) authorize(ctx context.Context) error {
	if c.AccessToken != "" {
		return nil
	}
	var err error
	switch c.Provider {
	case ProviderDatabricks:
		c.AccessToken, err = databricksToken(ctx, c.BaseURL, c.ClientID, c.ClientSecret)
	case ProviderVertex:
		c.AccessToken, err = googleServiceAccountToken(ctx, c.ClientEmail, c.PrivateKey)
	}
	return err
}

// embedVertex calls a Vertex AI text-embedding model's predict method.
func (c embedConfig) embedVertex(ctx context.Context, texts []string, p purpose) ([][]float64, error) {
	task := "RETRIEVAL_DOCUMENT"
	if p == purposeQuery {
		task = "RETRIEVAL_QUERY"
	}
	instances := make([]map[string]interface{}, len(texts))
	for i, t := range texts {
		instances[i] = map[string]interface{}{"content": t, "task_type": task}
	}
	body := map[string]interface{}{"instances": instances}
	if c.Dimensions > 0 {
		body["parameters"] = map[string]interface{}{"outputDimensionality": c.Dimensions}
	}
	endpoint := fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		c.BaseURL, url.PathEscape(c.ProjectID), url.PathEscape(c.Location), url.PathEscape(c.Model))
	var resp struct {
		Predictions []struct {
			Embeddings struct {
				Values []float64 `json:"values"`
			} `json:"embeddings"`
		} `json:"predictions"`
	}
	if err := doJSON(ctx, "POST", endpoint, map[string]string{"Authorization": bearer(c.AccessToken)}, body, &resp); err != nil {
		return nil, err
	}
	out := make([][]float64, len(resp.Predictions))
	for i, pr := range resp.Predictions {
		out[i] = pr.Embeddings.Values
	}
	return out, nil
}

// embedOracle runs the database's own ONNX model (VECTOR_EMBEDDING), one
// query per text, and reads the vector back as JSON text.
func (c embedConfig) embedOracle(ctx context.Context, texts []string) ([][]float64, error) {
	db, err := sql.Open(oracleDriverName, c.ConnectionString)
	if err != nil {
		return nil, fmt.Errorf("open oracle: %w", err)
	}
	defer db.Close()
	q := "SELECT FROM_VECTOR(VECTOR_EMBEDDING(" + c.Model + " USING :1 AS data) RETURNING CLOB) FROM dual"
	out := make([][]float64, 0, len(texts))
	for i, t := range texts {
		var raw string
		if err := db.QueryRowContext(ctx, q, t).Scan(&raw); err != nil {
			return nil, fmt.Errorf("text %d: %w", i, err)
		}
		var v []float64
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, fmt.Errorf("text %d: the database returned %q, not a vector", i, truncate(raw, 80))
		}
		out = append(out, v)
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// databricksToken trades a service principal's client id and secret for an
// access token (OAuth client credentials at the workspace's OIDC endpoint).
func databricksToken(ctx context.Context, host, clientID, secret string) (string, error) {
	form := url.Values{"grant_type": {"client_credentials"}, "scope": {"all-apis"}}
	req, err := http.NewRequestWithContext(ctx, "POST", host+"/oidc/v1/token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, secret)
	return fetchAccessToken(req, "databricks token")
}

// googleServiceAccountToken signs a JWT for the service account and trades
// it for a cloud-platform access token (OAuth 2.0 JWT bearer grant).
func googleServiceAccountToken(ctx context.Context, email, privateKey string) (string, error) {
	key, err := parseRSAPrivateKey(privateKey)
	if err != nil {
		return "", fmt.Errorf("private_key: %w", err)
	}
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]interface{}{
		"iss": email, "scope": googleCloudScope, "aud": googleTokenURL,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", fmt.Errorf("sign the service account JWT: %w", err)
	}
	form := url.Values{
		"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
		"assertion":  {unsigned + "." + base64.RawURLEncoding.EncodeToString(sig)},
	}
	req, err := http.NewRequestWithContext(ctx, "POST", googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return fetchAccessToken(req, "google token")
}

// parseRSAPrivateKey reads a PEM key (PKCS#8 or PKCS#1). A key pasted with
// literal "\n" sequences, as JSON key files and n8n credentials hold it,
// is accepted too.
func parseRSAPrivateKey(s string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(strings.TrimSpace(s), `\n`, "\n")))
	if block == nil {
		return nil, fmt.Errorf("not a PEM private key")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rk, ok := k.(*rsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("not an RSA key")
		}
		return rk, nil
	}
	k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not a PKCS#8 or PKCS#1 RSA key")
	}
	return k, nil
}

// fetchAccessToken sends a token request and reads access_token.
func fetchAccessToken(req *http.Request, what string) (string, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("%s: %w", what, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%s: HTTP %d: %s", what, resp.StatusCode, errorBodyEcho(data))
	}
	var tok struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(data, &tok); err != nil || tok.AccessToken == "" {
		return "", fmt.Errorf("%s: no access_token in the response", what)
	}
	return tok.AccessToken, nil
}
