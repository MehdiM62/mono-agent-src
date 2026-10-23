package orggrant

import "testing"

// A wildcard bind address is not a name a client can send as Host.
func TestEndpointURLUsesLoopbackForAWildcardBind(t *testing.T) {
	if got := EndpointURL("0.0.0.0:9322", "ep_x"); got != "http://127.0.0.1:9322/org-endpoint/ep_x" {
		t.Errorf("got %q", got)
	}
}
