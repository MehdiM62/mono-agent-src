package apipkg

import (
	"strings"
	"testing"
)

func TestScopeCheckWiredToUmbrellaConnections(t *testing.T) {
	if ScopeCheck == nil {
		t.Fatal("ScopeCheck is not wired")
	}
	cred := map[string]any{"oauth_platform": "google", "access_token": "t",
		"scope": "https://www.googleapis.com/auth/spreadsheets"}
	err := ScopeCheck(cred, "calendar")
	if err == nil || !strings.Contains(err.Error(), "--scopes") {
		t.Errorf("calendar on a sheets-only google connection: %v", err)
	}
	if err := ScopeCheck(map[string]any{"access_token": "t"}, "calendar"); err != nil {
		t.Errorf("a non-umbrella connection must pass: %v", err)
	}
}
