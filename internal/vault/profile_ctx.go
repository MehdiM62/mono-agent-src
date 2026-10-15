package vault

import "context"

// ProfileIDInContext reports whether ctx carries a profile at all, for a
// caller that must tell "no profile" from an error.
func ProfileIDInContext(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(profileCtxKey{}).(string)
	return v, ok && v != ""
}
