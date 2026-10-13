package dbnodes

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/workflow"
)

func TestSnowflakeInsertPollsAndBinds(t *testing.T) {
	var polls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer pat-123" || r.Header.Get("X-Snowflake-Authorization-Token-Type") != "PROGRAMMATIC_ACCESS_TOKEN" {
			t.Errorf("auth headers = %v", r.Header)
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v2/statements":
			var body map[string]interface{}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			if body["statement"] != "INSERT INTO users (age, name) VALUES (?, ?)" || body["warehouse"] != "WH" || body["database"] != "DB" {
				t.Errorf("body = %s", b)
			}
			binds := body["bindings"].(map[string]interface{})
			if binds["1"].(map[string]interface{})["type"] != "FIXED" || binds["2"].(map[string]interface{})["value"] != "Ada" {
				t.Errorf("bindings = %v", binds)
			}
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"statementHandle":"h1"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v2/statements/h1":
			atomic.AddInt32(&polls, 1)
			_, _ = w.Write([]byte(`{"statementHandle":"h1","resultSetMetaData":{"rowType":[{"name":"number of rows inserted","type":"fixed"}],"partitionInfo":[{}]},"data":[["1"]],"stats":{"numRowsInserted":1}}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
		}
	}))
	defer srv.Close()
	out, err := (&SnowflakeNode{}).Execute(context.Background(), items(map[string]interface{}{"name": "Ada", "age": float64(36)}), map[string]interface{}{
		"account": "xy12345", "base_url": srv.URL, "token": "pat-123", "token_type": "programmatic_access_token",
		"database": "DB", "warehouse": "WH", "operation": "insert", "table": "users",
	})
	if err != nil {
		t.Fatal(err)
	}
	if polls != 1 {
		t.Errorf("polls = %d", polls)
	}
	got := mainItems(t, out)
	if len(got) != 1 || got[0].JSON["success"] != true || got[0].JSON["rows_affected"] != int64(1) {
		t.Errorf("got %+v", got)
	}
}

func TestSnowflakeSelectTypedRowsAndPartitions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		meta := `"resultSetMetaData":{"rowType":[{"name":"ID","type":"fixed","scale":0},{"name":"OK","type":"boolean"},{"name":"V","type":"variant"},{"name":"N","type":"text"}],"partitionInfo":[{},{}]}`
		if r.URL.Query().Get("partition") == "1" {
			_, _ = w.Write([]byte(`{"data":[["2","false","[1]",null]]}`))
			return
		}
		_, _ = w.Write([]byte(`{"statementHandle":"h",` + meta + `,"data":[["1","true","{\"a\":1}","x"]]}`))
	}))
	defer srv.Close()
	out, err := (&SnowflakeNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"account": "acme-main", "base_url": srv.URL, "token": "oauth", "query": "SELECT * FROM t",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := mainItems(t, out)
	if len(got) != 2 || got[0].JSON["ID"] != int64(1) || got[0].JSON["OK"] != true || got[1].JSON["N"] != nil {
		t.Fatalf("got %+v", got)
	}
	if v := got[0].JSON["V"].(map[string]interface{}); v["a"] != float64(1) {
		t.Errorf("variant = %v", v)
	}
}

func TestSnowflakeErrorsAndAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"code":"002003","message":"Table 'X' does not exist"}`))
	}))
	defer srv.Close()
	_, err := (&SnowflakeNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{
		"account": "a", "base_url": srv.URL, "token": "t", "query": "SELECT 1",
	})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v", err)
	}
	if _, err := (&SnowflakeNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{"account": "a"}); err == nil || !strings.Contains(err.Error(), "private_key") {
		t.Errorf("missing auth err = %v", err)
	}
	if _, err := (&SnowflakeNode{}).Execute(context.Background(), workflow.NodeInput{}, map[string]interface{}{"token": "t"}); err == nil || !strings.Contains(err.Error(), "account") {
		t.Errorf("missing account err = %v", err)
	}
}

func TestSnowflakeJWTClaims(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	p := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
	tok, err := snowflakeJWT("xy12345.us-east-1", "bob", p, time.Unix(1000, 0))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("token = %q", tok)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var claims map[string]interface{}
	_ = json.Unmarshal(raw, &claims)
	if claims["sub"] != "XY12345.BOB" || !strings.HasPrefix(claims["iss"].(string), "XY12345.BOB.SHA256:") || claims["exp"].(float64) <= claims["iat"].(float64) {
		t.Errorf("claims = %v", claims)
	}
	if _, err := snowflakeJWT("a", "b", "not pem", time.Now()); err == nil {
		t.Error("bad PEM: want error")
	}
}
