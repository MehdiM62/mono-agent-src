package connections

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func saveTestConn(t *testing.T, s *Store, platform, account string, data map[string]interface{}) *Connection {
	t.Helper()
	c := &Connection{Platform: platform, Method: MethodOAuth, AccountID: account, Data: data, Status: "active"}
	if err := s.Save(context.Background(), c); err != nil {
		t.Fatalf("save %s: %v", platform, err)
	}
	return c
}

func googleData(token string, groups ...string) map[string]interface{} {
	scopes, _ := ResolveScopes("google", groups)
	return map[string]interface{}{"access_token": token, "oauth_platform": "google", "scope": strings.Join(scopes, " ")}
}

// Every node whose credential_platform is a legacy per-product platform
// resolves its own connection, an umbrella connection, or — when both
// exist — its own first, with no migration of saved rows.
func TestGetOrResolveLegacyAndUmbrellaCompatibility(t *testing.T) {
	ctx := context.Background()
	products := map[string]string{"google_sheets": "google", "gmail": "google", "google_drive": "google", "youtube": "google", "outlook": "microsoft"}
	for product, umbrella := range products {
		t.Run(product, func(t *testing.T) {
			s := NewStore(newTestDB(t))

			if _, err := s.GetOrResolve(ctx, product, ""); err == nil {
				t.Fatal("empty store must not resolve")
			}

			umb := saveTestConn(t, s, umbrella, "me@example.com", map[string]interface{}{"access_token": "umbrella-token", "oauth_platform": umbrella})
			got, err := s.GetOrResolve(ctx, product, "")
			if err != nil || got == nil || got.ID != umb.ID {
				t.Fatalf("umbrella-only resolve = %+v, %v", got, err)
			}
			if got.Data["access_token"] != "umbrella-token" {
				t.Errorf("umbrella token not returned: %v", got.Data["access_token"])
			}

			old := saveTestConn(t, s, product, "me@example.com", map[string]interface{}{"access_token": "legacy-token"})
			got, err = s.GetOrResolve(ctx, product, "")
			if err != nil || got.ID != old.ID {
				t.Fatalf("legacy connection must win: got %+v, %v", got, err)
			}
			// Resolving by ID works for both kinds.
			for _, id := range []string{old.ID, umb.ID} {
				if c, err := s.GetOrResolve(ctx, id, ""); err != nil || c.ID != id {
					t.Errorf("resolve by id %s: %+v, %v", id, c, err)
				}
			}
			usable, err := s.ListUsableByPlatform(ctx, product, "default")
			if err != nil || len(usable) != 2 || usable[0].ID != old.ID || usable[1].ID != umb.ID {
				t.Errorf("ListUsableByPlatform = %d conns, %v", len(usable), err)
			}
			// Another profile sees neither.
			if _, err := s.GetOrResolve(ctx, product, "other"); err == nil {
				t.Error("cross-profile resolution")
			}
		})
	}
}

func TestGetOrResolvePrefersUmbrellaWithNeededScopes(t *testing.T) {
	ctx := context.Background()
	s := NewStore(newTestDB(t))
	saveTestConn(t, s, "google", "a@example.com", googleData("sheets-only", "sheets"))
	withGmail := saveTestConn(t, s, "google", "b@example.com", googleData("has-gmail", "gmail"))
	got, err := s.GetOrResolve(ctx, "gmail", "")
	if err != nil || got.ID != withGmail.ID {
		t.Fatalf("want the connection granted gmail, got %+v, %v", got, err)
	}
}

func TestLookupOAuthClientFallbackOrder(t *testing.T) {
	ctx := context.Background()
	envs := []string{"GOOGLE", "GOOGLE_SHEETS", "GMAIL", "GOOGLE_DRIVE", "YOUTUBE", "MICROSOFT", "OUTLOOK"}
	clear := func(t *testing.T) {
		for _, e := range envs {
			t.Setenv("MONOAGENT_"+e+"_CLIENT_ID", "")
			t.Setenv("MONOAGENT_"+e+"_CLIENT_SECRET", "")
		}
	}

	t.Run("nothing configured", func(t *testing.T) {
		clear(t)
		s := NewStore(newTestDB(t))
		if id, _ := s.lookupOAuthClient(ctx, "google", "default"); id != "" {
			t.Errorf("id = %q", id)
		}
	})
	t.Run("stored legacy client", func(t *testing.T) {
		clear(t)
		s := NewStore(newTestDB(t))
		_ = s.SaveOAuthClient(ctx, "youtube", "default", "db-youtube", "db-yt-secret")
		_ = s.SaveOAuthClient(ctx, "gmail", "default", "db-gmail", "db-gm-secret")
		if id, sec := s.lookupOAuthClient(ctx, "google", "default"); id != "db-gmail" || sec != "db-gm-secret" {
			t.Errorf("got %q/%q, want the gmail client (members order)", id, sec)
		}
	})
	t.Run("legacy env beats stored legacy, sheets first", func(t *testing.T) {
		clear(t)
		s := NewStore(newTestDB(t))
		_ = s.SaveOAuthClient(ctx, "gmail", "default", "db-gmail", "")
		t.Setenv("MONOAGENT_GMAIL_CLIENT_ID", "env-gmail")
		t.Setenv("MONOAGENT_GOOGLE_SHEETS_CLIENT_ID", "env-sheets")
		t.Setenv("MONOAGENT_GOOGLE_SHEETS_CLIENT_SECRET", "env-sheets-secret")
		if id, sec := s.lookupOAuthClient(ctx, "google", "default"); id != "env-sheets" || sec != "env-sheets-secret" {
			t.Errorf("got %q/%q", id, sec)
		}
	})
	t.Run("umbrella env beats legacy", func(t *testing.T) {
		clear(t)
		s := NewStore(newTestDB(t))
		t.Setenv("MONOAGENT_GMAIL_CLIENT_ID", "env-gmail")
		t.Setenv("MONOAGENT_GOOGLE_CLIENT_ID", "env-google")
		t.Setenv("MONOAGENT_GOOGLE_CLIENT_SECRET", "env-google-secret")
		if id, sec := s.lookupOAuthClient(ctx, "google", "default"); id != "env-google" || sec != "env-google-secret" {
			t.Errorf("got %q/%q", id, sec)
		}
	})
	t.Run("stored umbrella client beats env", func(t *testing.T) {
		clear(t)
		s := NewStore(newTestDB(t))
		t.Setenv("MONOAGENT_GOOGLE_CLIENT_ID", "env-google")
		t.Setenv("MONOAGENT_GOOGLE_CLIENT_SECRET", "env-google-secret")
		_ = s.SaveOAuthClient(ctx, "google", "default", "db-google", "")
		if id, sec := s.lookupOAuthClient(ctx, "google", "default"); id != "db-google" || sec != "env-google-secret" {
			t.Errorf("got %q/%q (stored id, env secret fills the empty stored one)", id, sec)
		}
	})
	t.Run("microsoft falls back to outlook", func(t *testing.T) {
		clear(t)
		s := NewStore(newTestDB(t))
		t.Setenv("MONOAGENT_OUTLOOK_CLIENT_ID", "env-outlook")
		if id, _ := s.lookupOAuthClient(ctx, "microsoft", "default"); id != "env-outlook" {
			t.Errorf("got %q", id)
		}
	})
	t.Run("legacy platforms do not borrow", func(t *testing.T) {
		clear(t)
		s := NewStore(newTestDB(t))
		t.Setenv("MONOAGENT_GOOGLE_CLIENT_ID", "env-google")
		if id, _ := s.lookupOAuthClient(ctx, "gmail", "default"); id != "" {
			t.Errorf("gmail borrowed %q", id)
		}
	})
}

// withPlatformTokenURL points a registered platform's token endpoint at url
// for the duration of the test.
func withPlatformTokenURL(t *testing.T, platform, url string) {
	t.Helper()
	orig := Registry[platform]
	p := orig
	o := *orig.OAuth
	o.TokenURL = url
	p.OAuth = &o
	Registry[platform] = p
	t.Cleanup(func() { Registry[platform] = orig })
}

func TestRefreshTokenUmbrella(t *testing.T) {
	for _, tc := range []struct{ platform, legacyEnv, grantedScope string }{
		{"google", "MONOAGENT_GMAIL_CLIENT_ID", "openid https://www.googleapis.com/auth/gmail.modify https://www.googleapis.com/auth/calendar.events"},
		{"microsoft", "MONOAGENT_OUTLOOK_CLIENT_ID", "https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/Chat.ReadWrite"},
	} {
		t.Run(tc.platform, func(t *testing.T) {
			var gotForm map[string]string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Errorf("method = %s", r.Method)
				}
				_ = r.ParseForm()
				gotForm = map[string]string{}
				for k := range r.PostForm {
					gotForm[k] = r.PostForm.Get(k)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"access_token":"new-access","expires_in":3600,"token_type":"Bearer","scope":"` + tc.grantedScope + `"}`))
			}))
			defer srv.Close()
			withPlatformTokenURL(t, tc.platform, srv.URL)
			t.Setenv("MONOAGENT_"+strings.ToUpper(tc.platform)+"_CLIENT_ID", "")
			t.Setenv(tc.legacyEnv, "legacy-client")

			ctx := context.Background()
			s := NewStore(newTestDB(t))
			conn := saveTestConn(t, s, tc.platform, "me@example.com", map[string]interface{}{
				"access_token": "old-access", "refresh_token": "the-refresh", "oauth_platform": tc.platform,
				"scope": "openid", "expires_at": "2000-01-01T00:00:00Z",
			})

			// Resolving a stale umbrella connection refreshes it.
			got, err := s.GetOrResolve(ctx, conn.ID, "")
			if err != nil {
				t.Fatal(err)
			}
			if gotForm["grant_type"] != "refresh_token" || gotForm["refresh_token"] != "the-refresh" || gotForm["client_id"] != "legacy-client" {
				t.Errorf("refresh form = %v", gotForm)
			}
			if got.Data["access_token"] != "new-access" {
				t.Errorf("access_token = %v", got.Data["access_token"])
			}
			stored, _ := s.Get(ctx, conn.ID)
			if stored.Data["access_token"] != "new-access" || stored.Data["scope"] != tc.grantedScope || stored.Data["refresh_token"] != "the-refresh" {
				t.Errorf("stored after refresh: access=%v scope=%v refresh=%v", stored.Data["access_token"], stored.Data["scope"], stored.Data["refresh_token"])
			}
		})
	}
}

func TestRefreshTokenLegacyKeepsScope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"new","expires_in":3600,"scope":"changed"}`))
	}))
	defer srv.Close()
	withPlatformTokenURL(t, "gmail", srv.URL)
	t.Setenv("MONOAGENT_GMAIL_CLIENT_ID", "gm")
	s := NewStore(newTestDB(t))
	conn := saveTestConn(t, s, "gmail", "me", map[string]interface{}{"access_token": "old", "refresh_token": "r", "scope": "orig"})
	if err := s.RefreshToken(context.Background(), conn); err != nil {
		t.Fatal(err)
	}
	if conn.Data["scope"] != "orig" {
		t.Errorf("legacy scope rewritten to %v", conn.Data["scope"])
	}
}

func TestAdoptSameAccountConnection(t *testing.T) {
	ctx := context.Background()
	db := newTestDB(t)
	m := &Manager{store: NewStore(db)}
	existing := saveTestConn(t, m.store, "google", "Me@Example.com", googleData("old", "sheets"))
	saveTestConn(t, m.store, "google", "other@example.com", googleData("x", "gmail"))

	fresh := &Connection{Platform: "google", Method: MethodOAuth, Data: googleData("new", "sheets", "calendar")}
	m.adoptSameAccountConnection(ctx, fresh, "me@example.com")
	if fresh.ID != existing.ID {
		t.Fatalf("re-consent must update the existing connection in place: id %q, want %q", fresh.ID, existing.ID)
	}
	fresh.AccountID = "me@example.com"
	if err := m.store.Save(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	all, _ := m.store.ListByPlatform(ctx, "google", "default")
	if len(all) != 2 {
		t.Errorf("connections = %d, want 2", len(all))
	}
	stored, _ := m.store.Get(ctx, existing.ID)
	if CheckCredentialScopes(stored.Data, "calendar") != nil {
		t.Error("updated connection should now carry calendar")
	}

	newAcct := &Connection{Platform: "google", Method: MethodOAuth}
	m.adoptSameAccountConnection(ctx, newAcct, "third@example.com")
	if newAcct.ID != "" {
		t.Error("a new account must get a new connection")
	}
}

func TestRecordUmbrellaGrant(t *testing.T) {
	c := &Connection{Data: map[string]interface{}{"scope": ""}}
	recordUmbrellaGrant(c, "google", []string{"openid", "x"})
	if c.Data["oauth_platform"] != "google" || c.Data["scope"] != "openid x" {
		t.Errorf("data = %v", c.Data)
	}
	c = &Connection{Data: map[string]interface{}{"scope": "granted"}}
	recordUmbrellaGrant(c, "microsoft", []string{"req"})
	if c.Data["scope"] != "granted" {
		t.Errorf("provider-reported scope overwritten: %v", c.Data["scope"])
	}
	c = &Connection{Data: map[string]interface{}{}}
	recordUmbrellaGrant(c, "gmail", []string{"req"})
	if len(c.Data) != 0 {
		t.Errorf("legacy connection marked: %v", c.Data)
	}
}
