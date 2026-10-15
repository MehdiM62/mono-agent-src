package httpapi

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"

	"github.com/monoes/mono-agent/internal/account"
	"github.com/monoes/mono-agent/internal/accountdoor"
	"github.com/monoes/mono-agent/internal/profiledir"
)

// Authentication. One server serves every profile, and each profile has its
// own bearer token in its own vault (token.go). A request names its profile in
// the path (/profiles/{profile}/...), and the bearer is checked against the
// token of THAT profile only: a token is never an answer for another profile.
//
// A caller that fails to prove it holds the path profile's token gets one and
// the same 401 whatever the reason (no token, wrong token, another profile's
// token, a profile that does not exist, a name that two profiles share), so
// the response never tells an unauthenticated caller which profiles exist.

type profileCtxKey struct{}

// profileOf is the profile the request was authenticated for. Only handlers
// behind profileAuth may call it.
func profileOf(r *http.Request) string {
	id, _ := r.Context().Value(profileCtxKey{}).(string)
	return id
}

func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer realm=monoagentcli-httpapi")
	writeError(w, http.StatusUnauthorized, "missing or invalid bearer credential")
}

// profileAuth wraps h with the profile-scoped bearer check. h reads the
// profile with profileOf.
func (s *Server) profileAuth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A locked account is refused before the vault is opened or the bearer is read.
		if err := account.Require(r.Context()); err != nil {
			accountdoor.WriteUnauthorized(w, err)
			return
		}
		got := bearerCredential(r)
		if got == "" {
			writeUnauthorized(w)
			return
		}
		ctx := r.Context()
		candidates, err := candidateProfiles(ctx, s.rt.db.DB, r.PathValue("profile"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "resolve profile: "+err.Error())
			return
		}
		if len(candidates) == 0 {
			// Spend the comparison a real profile would cost, so the answer
			// does not differ in what it computes.
			constantTimeEqual(got, s.decoyToken)
			writeUnauthorized(w)
			return
		}
		var matched []string
		for _, id := range candidates {
			want, err := ensureToken(ctx, s.rt.db.DB, id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "resolve bearer credential: "+err.Error())
				return
			}
			if constantTimeEqual(got, want) {
				matched = append(matched, id)
			}
		}
		switch {
		case len(matched) == 0:
			writeUnauthorized(w)
		case len(candidates) > 1:
			// The caller holds the token of one profile of that name: it may
			// be told the name is shared, and that the id is what to use.
			writeError(w, http.StatusConflict, "profile name is ambiguous: use the profile id")
		default:
			h(w, r.WithContext(context.WithValue(ctx, profileCtxKey{}, matched[0])))
		}
	})
}

// anyProfileAuth wraps h, a handler of a global route (/nodes), with a bearer
// check that accepts the token of any profile. It reads tokens, never creates
// them.
func (s *Server) anyProfileAuth(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := account.Require(r.Context()); err != nil {
			accountdoor.WriteUnauthorized(w, err)
			return
		}
		got := bearerCredential(r)
		if got == "" {
			writeUnauthorized(w)
			return
		}
		ids, err := allProfileIDs(r.Context(), s.rt.db.DB)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "resolve profiles: "+err.Error())
			return
		}
		ok := constantTimeEqual(got, s.decoyToken) // false; keeps the work up when there are no tokens
		for _, id := range ids {                   // every token is compared: no early exit
			want, err := existingToken(r.Context(), s.rt.db.DB, id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "resolve bearer credential: "+err.Error())
				return
			}
			if want != "" && constantTimeEqual(got, want) {
				ok = true
			}
		}
		if !ok {
			writeUnauthorized(w)
			return
		}
		h(w, r)
	})
}

// candidateProfiles resolves the path segment like the CLI's --profile: an
// exact profile id first (the reserved machine scope is never a profile),
// else every profile with that name, case-insensitively. More than one
// candidate means the name is ambiguous.
func candidateProfiles(ctx context.Context, db *sql.DB, idOrName string) ([]string, error) {
	if idOrName == "" {
		return nil, nil
	}
	if idOrName != profiledir.MachineProfileID {
		var id string
		err := db.QueryRowContext(ctx, `SELECT id FROM profiles WHERE id = ?`, idOrName).Scan(&id)
		if err == nil {
			return []string{id}, nil
		}
		if err != sql.ErrNoRows {
			return nil, err
		}
	}
	rows, err := db.QueryContext(ctx, `SELECT id FROM profiles WHERE LOWER(name) = LOWER(?) ORDER BY id`, idOrName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func allProfileIDs(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT id FROM profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ensureTokens gives every existing profile its token, so that `secret list`
// shows it from the first start. Best effort: a profile created later gets its
// token on its first request.
func (s *Server) ensureTokens(ctx context.Context) {
	if account.Require(ctx) != nil { // a locked server does not open the vault
		return
	}
	ids, err := allProfileIDs(ctx, s.rt.db.DB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "httpapi: warning: listing profiles: %v\n", err)
		return
	}
	for _, id := range ids {
		if id == profiledir.MachineProfileID {
			continue
		}
		if _, err := ensureToken(ctx, s.rt.db.DB, id); err != nil {
			fmt.Fprintf(os.Stderr, "httpapi: warning: profile %s: %v\n", id, err)
		}
	}
}
