package httpnodes

import (
	"context"
	"testing"

	"github.com/monoes/mono-agent/internal/workflow"
)

// A next page that keeps the host but drops https for http, or changes the
// port, goes without the request's auth, as a redirect there does.
func TestPaginationNextURLSchemeDowngradeDropsAuth(t *testing.T) {
	cases := []struct {
		next     string
		wantAuth bool
	}{
		{"https://api.example.com/p2", true},
		{"http://api.example.com/p2", false},
		{"https://api.example.com:8443/p2", false},
	}
	for _, c := range cases {
		r := &requestRun{}
		cfg := map[string]interface{}{"url": "https://api.example.com/a"}
		ob, err := r.prepare(context.Background(), cfg, workflow.Item{}, authPlan{tok: "tok-1"}, pageOverrides{url: c.next}, -1)
		if err != nil {
			t.Fatal(err)
		}
		if got := ob.auth.tok != ""; got != c.wantAuth {
			t.Errorf("next %s: auth kept = %v, want %v", c.next, got, c.wantAuth)
		}
	}
}
