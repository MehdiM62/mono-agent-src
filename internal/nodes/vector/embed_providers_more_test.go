package vector

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/zalando/go-keyring"

	"github.com/monoes/mono-agent/internal/connections"
	"github.com/monoes/mono-agent/internal/testdb"
	"github.com/monoes/mono-agent/internal/vault"
)

func TestEmbedDatabricksServicePrincipal(t *testing.T) {
	var tokenCalls int
	srv, reqs := embedFakeServer(t, func(r *http.Request, body map[string]interface{}) (int, interface{}) {
		if r.URL.Path == "/oidc/v1/token" {
			tokenCalls++
			return 200, map[string]interface{}{"access_token": "sp-token"}
		}
		in := body["input"].([]interface{})
		data := []map[string]interface{}{}
		for i := range in {
			data = append(data, map[string]interface{}{"index": i, "embedding": []float64{float64(i + 1)}})
		}
		return 200, map[string]interface{}{"data": data}
	})
	vecs := runEmbed(t, map[string]interface{}{
		"provider": "databricks", "host": srv.URL + "/", "client_id": "cid", "client_secret": "sec",
		"model": "databricks-gte-large-en", "batch_size": float64(1),
	}, "a", "b")
	if !reflect.DeepEqual(vecs, [][]float64{{1}, {1}}) {
		t.Fatalf("vectors %v", vecs)
	}
	if tokenCalls != 1 {
		t.Fatalf("token fetched %d times, want once per run", tokenCalls)
	}
	tok := (*reqs)[0]
	if tok.Method != "POST" || tok.Header.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("cid:sec")) {
		t.Fatalf("token request %+v", tok)
	}
	r := (*reqs)[1]
	if r.Path != "/ai-gateway/openai/v1/embeddings" || r.Header.Get("Authorization") != "Bearer sp-token" ||
		r.Body["model"] != "databricks-gte-large-en" {
		t.Fatalf("embed request %+v", r)
	}
}

func TestEmbedDatabricksAccessToken(t *testing.T) {
	srv, reqs := embedFakeServer(t, func(r *http.Request, body map[string]interface{}) (int, interface{}) {
		return 200, map[string]interface{}{"data": []map[string]interface{}{{"index": 0, "embedding": []float64{7}}}}
	})
	runEmbed(t, map[string]interface{}{"provider": "databricks", "host": srv.URL, "access_token": "pat", "model": "m"}, "x")
	if len(*reqs) != 1 || (*reqs)[0].Header.Get("Authorization") != "Bearer pat" {
		t.Fatalf("requests %+v", *reqs)
	}
}

func TestEmbedVertexServiceAccount(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemKey := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	var assertion url.Values
	srv, reqs := embedFakeServer(t, func(r *http.Request, body map[string]interface{}) (int, interface{}) {
		preds := []map[string]interface{}{}
		for i := range body["instances"].([]interface{}) {
			preds = append(preds, map[string]interface{}{"embeddings": map[string]interface{}{"values": []float64{float64(i)}}})
		}
		return 200, map[string]interface{}{"predictions": preds}
	})
	tsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		assertion = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.test"}`))
	}))
	t.Cleanup(tsrv.Close)
	old := googleTokenURL
	googleTokenURL = tsrv.URL + "/token"
	t.Cleanup(func() { googleTokenURL = old })

	// The key as JSON key files and n8n hold it: newlines escaped.
	escaped := strings.ReplaceAll(pemKey, "\n", `\n`)
	vecs := runEmbed(t, map[string]interface{}{
		"provider": "vertex", "base_url": srv.URL, "project_id": "proj-1", "location": "europe-west4",
		"client_email": "sa@proj-1.iam.gserviceaccount.com", "private_key": escaped, "dimensions": float64(256),
	}, "a", "b")
	if !reflect.DeepEqual(vecs, [][]float64{{0}, {1}}) {
		t.Fatalf("vectors %v", vecs)
	}
	if assertion.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
		t.Fatalf("grant %v", assertion)
	}
	parts := strings.Split(assertion.Get("assertion"), ".")
	if len(parts) != 3 {
		t.Fatalf("assertion %q", assertion.Get("assertion"))
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("JWT signature: %v", err)
	}
	var claims map[string]interface{}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	_ = json.Unmarshal(raw, &claims)
	if claims["iss"] != "sa@proj-1.iam.gserviceaccount.com" || claims["scope"] != googleCloudScope || claims["aud"] != googleTokenURL {
		t.Fatalf("claims %v", claims)
	}
	r := (*reqs)[0]
	if r.Path != "/v1/projects/proj-1/locations/europe-west4/publishers/google/models/text-embedding-005:predict" ||
		r.Header.Get("Authorization") != "Bearer ya29.test" {
		t.Fatalf("predict request %+v", r)
	}
	inst := r.Body["instances"].([]interface{})[0].(map[string]interface{})
	if inst["content"] != "a" || inst["task_type"] != "RETRIEVAL_DOCUMENT" ||
		r.Body["parameters"].(map[string]interface{})["outputDimensionality"] != float64(256) {
		t.Fatalf("predict body %v", r.Body)
	}
}

func TestVertexDefaultsAndErrors(t *testing.T) {
	c, err := parseEmbedConfig(map[string]interface{}{"provider": "vertex", "project_id": "p", "access_token": "t", "location": "eu"}, "embedding.")
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://aiplatform.eu.rep.googleapis.com" || c.BatchSize != 5 || c.Model != "text-embedding-005" {
		t.Fatalf("config %+v", c)
	}
	if got := vertexBaseURL("global"); got != "https://aiplatform.googleapis.com" {
		t.Fatalf("global %s", got)
	}
	if got := vertexBaseURL("us-central1"); got != "https://us-central1-aiplatform.googleapis.com" {
		t.Fatalf("regional %s", got)
	}
	for _, tc := range []struct {
		cfg  map[string]interface{}
		want string
	}{
		{map[string]interface{}{"provider": "vertex", "access_token": "t"}, "embedding.project_id is required"},
		{map[string]interface{}{"provider": "vertex", "project_id": "p"}, "embedding.access_token, or embedding.client_email"},
		{map[string]interface{}{"provider": "databricks", "access_token": "t", "model": "m"}, "embedding.host is required"},
		{map[string]interface{}{"provider": "databricks", "host": "https://x", "access_token": "t"}, "embedding.model (the serving endpoint name)"},
		{map[string]interface{}{"provider": "databricks", "host": "https://x", "model": "m"}, "client_secret of a service principal"},
		{map[string]interface{}{"provider": "oracle", "model": "m"}, "embedding.connection_string is required"},
		{map[string]interface{}{"provider": "oracle", "connection_string": "x", "model": "a b"}, "ONNX model"},
	} {
		if _, err := parseEmbedConfig(tc.cfg, "embedding."); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%v: err %v, want %q", tc.cfg, err, tc.want)
		}
	}
	if _, err := parseRSAPrivateKey("nope"); err == nil {
		t.Error("garbage key accepted")
	}
}

func TestEmbedOracleInDatabase(t *testing.T) {
	s := useOracleFake(t, "ora-embed")
	s.cols = []string{"v"}
	s.rows = [][]driver.Value{{"[0.25,-1,3]"}}
	vecs := runEmbed(t, map[string]interface{}{"provider": "oracle", "connection_string": "ora-embed", "model": "doc_model"}, "alpha", "beta")
	if !reflect.DeepEqual(vecs, [][]float64{{0.25, -1, 3}, {0.25, -1, 3}}) {
		t.Fatalf("vectors %v", vecs)
	}
	want := "SELECT FROM_VECTOR(VECTOR_EMBEDDING(doc_model USING :1 AS data) RETURNING CLOB) FROM dual"
	if len(s.calls) != 2 || s.calls[0].Query != want || s.calls[1].Args[0] != "beta" {
		t.Fatalf("calls %#v", s.calls)
	}
}

func TestEmbedOracleBadReply(t *testing.T) {
	s := useOracleFake(t, "ora-embed-bad")
	s.cols = []string{"v"}
	s.rows = [][]driver.Value{{"not a vector"}}
	ec, err := parseEmbedConfig(map[string]interface{}{"provider": "oracle", "connection_string": "ora-embed-bad", "model": "m"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ec.embed(t.Context(), []string{"x"}, purposeDocument); err == nil || !strings.Contains(err.Error(), "not a vector") {
		t.Fatalf("err %v", err)
	}
}

func TestN8nEmbeddingsNewProviders(t *testing.T) {
	cases := []struct {
		typ    string
		params map[string]interface{}
		want   map[string]interface{}
	}{
		{"embeddingsDatabricks", map[string]interface{}{"model": map[string]interface{}{"__rl": true, "mode": "list", "value": "gte"},
			"options": map[string]interface{}{"batchSize": float64(16)}},
			map[string]interface{}{"provider": ProviderDatabricks, "model": "gte", "batch_size": float64(16)}},
		{"embeddingsOracleDb", map[string]interface{}{"model": "doc_model"}, map[string]interface{}{"provider": ProviderOracle, "model": "doc_model"}},
		{"embeddingsGoogleVertex", map[string]interface{}{"modelName": "text-embedding-005", "location": "global",
			"projectId": map[string]interface{}{"__rl": true, "mode": "id", "value": "proj"}},
			map[string]interface{}{"provider": ProviderVertex, "model": "text-embedding-005", "project_id": "proj", "location": "global"}},
	}
	for _, tc := range cases {
		got, _, err := ConvertN8nParams(N8nPrefix+tc.typ, tc.params)
		if err != nil {
			t.Fatalf("%s: %v", tc.typ, err)
		}
		if got.Type != "vector.embed" || !reflect.DeepEqual(got.Config, tc.want) {
			t.Errorf("%s: %s %v, want %v", tc.typ, got.Type, got.Config, tc.want)
		}
	}
}

// A vector.store embedding block naming a google_service_account
// connection signs in with the connection's key and uses its project and
// location; the node's own location wins.
func TestVertexEmbeddingUsesServiceAccountConnection(t *testing.T) {
	keyring.MockInit()
	db := testdb.Open(t).DB
	ctx := vault.ContextWithProfileID(vault.ContextWithDB(context.Background(), db), "default")
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	conn := &connections.Connection{ProfileID: "default", Platform: "google_service_account", Method: connections.MethodAPIKey, Label: "sa",
		Data: map[string]interface{}{"client_email": "sa@proj.iam.gserviceaccount.com",
			"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})),
			"project_id":  "proj-from-conn", "location": "us-east1"}}
	if err := connections.NewStore(db).Save(ctx, conn); err != nil {
		t.Fatal(err)
	}
	var tokenAsked bool
	tsrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenAsked = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.conn"}`))
	}))
	t.Cleanup(tsrv.Close)
	old := googleTokenURL
	googleTokenURL = tsrv.URL
	t.Cleanup(func() { googleTokenURL = old })
	srv, reqs := embedFakeServer(t, func(r *http.Request, body map[string]interface{}) (int, interface{}) {
		return 200, map[string]interface{}{"predictions": []map[string]interface{}{{"embeddings": map[string]interface{}{"values": []float64{1}}}}}
	})
	cfg := map[string]interface{}{"provider": "vertex", "credential_id": "google_service_account", "location": "europe-west4", "base_url": srv.URL}
	if err := resolveNestedCredential(ctx, cfg); err != nil {
		t.Fatal(err)
	}
	ec, err := parseEmbedConfig(cfg, "embedding.")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ec.embed(ctx, []string{"x"}, purposeQuery); err != nil {
		t.Fatal(err)
	}
	r := (*reqs)[0]
	if !tokenAsked || r.Header.Get("Authorization") != "Bearer ya29.conn" ||
		r.Path != "/v1/projects/proj-from-conn/locations/europe-west4/publishers/google/models/text-embedding-005:predict" {
		t.Fatalf("token %v, request %+v", tokenAsked, r)
	}
}
