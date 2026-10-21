package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/agenttokens"
)

func runWithFullOutputs(t *testing.T, s *Server, tok string) (int, string) {
	t.Helper()
	body := `{"input":{"password":"hunter2","api_key":"sk-abc"}}`
	req, _ := http.NewRequest("POST", "/profiles/default/workflows/wf1/run", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set(fullOutputsHeader, "1")
	rw := httptest.NewRecorder()
	s.Handler().ServeHTTP(rw, req)
	return rw.Code, rw.Body.String()
}

// X-Full-Outputs switches redaction off only for the profile's own bearer (scope full):
// no agent token, whatever its scope, gets unredacted outputs.
func TestOnlyTheFullScopeGetsFullOutputs(t *testing.T) {
	s := newTestServer(t, true)
	seedWorkflow(t, s, "wf1", "default")
	for _, sc := range agenttokens.Issuable {
		tok := mint(t, s, "default", "t-"+string(sc), sc)
		code, body := runWithFullOutputs(t, s, tok)
		if strings.Contains(body, "hunter2") || strings.Contains(body, "sk-abc") {
			t.Errorf("scope %s got secrets in the clear (status %d): %s", sc, code, body)
		}
		if sc == agenttokens.ScopeRun && code != http.StatusOK {
			t.Errorf("a run token's run: status %d: %s", code, body)
		}
	}
	code, body := runWithFullOutputs(t, s, testToken(t, s, "default"))
	if code != http.StatusOK || !strings.Contains(body, "hunter2") || !strings.Contains(body, "sk-abc") {
		t.Errorf("the profile bearer with X-Full-Outputs: %d, want the values in the clear: %s", code, body)
	}
}

// The audit holds the escaped path: a request path with a newline and an escape byte
// does not put them in the log.
func TestAuditStoresTheEscapedPath(t *testing.T) {
	s := newTestServer(t, true)
	seedWorkflow(t, s, "wf1", "default")
	tok := mint(t, s, "default", "r", agenttokens.ScopeRun)
	doReq(t, s, "POST", "/profiles/default/workflows/%0a%1b[2Jevil/validate", tok, []byte(`{}`))
	var paths []string
	rows, err := s.rt.db.DB.Query(`SELECT path FROM agent_token_audit`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var p string
		_ = rows.Scan(&p)
		paths = append(paths, p)
	}
	rows.Close()
	if len(paths) == 0 {
		t.Fatal("nothing audited")
	}
	for _, p := range paths {
		if strings.ContainsAny(p, "\n\x1b") {
			t.Errorf("audit path holds control bytes: %q", p)
		}
	}
}

// The audit row is written before the handler runs; when it cannot be written a token is
// refused with 503 and the handler does not run, while the profile's own bearer is served.
func TestATokenIsRefusedWhenTheAuditCannotBeWritten(t *testing.T) {
	s := newTestServer(t, true)
	seedWorkflow(t, s, "wf1", "default")
	tok := mint(t, s, "default", "u", agenttokens.ScopeRun)
	if _, err := s.rt.db.DB.Exec(`ALTER TABLE agent_token_audit RENAME TO agent_token_audit_gone`); err != nil {
		t.Fatal(err)
	}
	rw := doReq(t, s, "POST", "/profiles/default/workflows/wf1/deactivate", tok, nil)
	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503: %s", rw.Code, rw.Body)
	}
	wf, err := s.rt.store.GetWorkflow(context.Background(), "wf1")
	if err != nil || !wf.IsActive {
		t.Fatalf("the handler ran without an audit row: %+v %v", wf, err)
	}
	if rw := doReq(t, s, "GET", "/profiles/default/workflows", testToken(t, s, "default"), nil); rw.Code != http.StatusOK {
		t.Errorf("the profile bearer: %d", rw.Code)
	}
}
