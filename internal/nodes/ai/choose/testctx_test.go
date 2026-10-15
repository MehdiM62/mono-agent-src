package choose

import (
	"context"

	"github.com/monoes/mono-agent/internal/vault"
)

// testCtx is a context that names the profile the node runs for.
func testCtx() context.Context {
	return vault.ContextWithProfileID(context.Background(), "p1")
}
