package vault

import "context"

// ExplicitProfileID is the profile ID stored by ContextWithProfileID, and
// false when the context carries none: unlike ProfileIDFromContext it never
// falls back to "default", for callers that must not guess a profile.
func ExplicitProfileID(ctx context.Context) (string, bool) {
	v, ok := ctx.Value(profileCtxKey{}).(string)
	return v, ok && v != ""
}
