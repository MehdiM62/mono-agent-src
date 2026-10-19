package vector

import (
	"context"
	"net/http"
	"testing"

	"github.com/monoes/mono-agent/internal/credhttp/credhttptest"
)

// An embedding provider's or vector store's request carries its key: a
// redirect to another host follows without it, and a 307 that would
// resend a body there isn't followed.
func TestVectorRequestsKeepTheKeyOnRedirect(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer vec-tok", "api-key": "vec-key"}
	get := credhttptest.NewPair(t, http.StatusFound, `{}`)
	var out map[string]interface{}
	if err := doJSON(context.Background(), http.MethodGet, get.From+"/collections", headers, nil, &out); err != nil {
		t.Fatal(err)
	}
	if got := get.Got(); len(got) != 1 || get.Leaked("vec-tok") != "" || get.Leaked("vec-key") != "" {
		t.Errorf("a GET's redirect carried %+v", got)
	}
	post := credhttptest.NewPair(t, http.StatusTemporaryRedirect, `{}`)
	_ = doJSON(context.Background(), http.MethodPost, post.From+"/embeddings", headers, map[string]interface{}{"input": "x"}, &out)
	if got := post.Got(); len(got) != 0 {
		t.Errorf("a POST's 307 reached the other host: %+v", got)
	}
}
