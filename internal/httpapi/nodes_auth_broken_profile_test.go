package httpapi

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

// One profile whose token cannot be read does not lock out the others on the
// global /nodes routes. A token that matches nowhere still gets the one
// uniform 401, whether or not some reads failed.
func TestNodesAuthSurvivesAProfileWithAnUnreadableToken(t *testing.T) {
	s := newTestServer(t, false)
	addProfile(t, s, "broken", "Broken")
	addProfile(t, s, "healthy", "Healthy")
	tokHealthy := testToken(t, s, "healthy")
	testToken(t, s, "broken")

	real := s.readToken
	s.readToken = func(ctx context.Context, id string) (string, error) {
		if id == "broken" {
			return "", errors.New("vault locked")
		}
		return real(ctx, id)
	}
	if rw := doReq(t, s, "GET", "/nodes", tokHealthy, nil); rw.Code != http.StatusOK {
		t.Fatalf("healthy token with a broken profile around: %d %s", rw.Code, rw.Body)
	}
	wantUnauthorized(t, "wrong token", doReq(t, s, "GET", "/nodes", "nope", nil))
}
