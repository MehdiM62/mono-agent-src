package service

import (
	"net/url"
	"testing"
)

// Saved comm.bluesky / comm.mastodon / comm.reddit configs, exactly as the
// old nodes' schemas produced them plus the keys their saved connections
// inject, must run unchanged on the canonical service.* nodes.

func TestLegacyBlueskyConfigs(t *testing.T) {
	srv := newLegacyServer(t, map[string]string{
		"POST /com.atproto.server.createSession": `{"accessJwt":"jwt-1","did":"did:plc:me"}`,
		"POST /com.atproto.repo.createRecord":    `{"uri":"at://did:plc:me/app.bsky.feed.post/1","cid":"c1"}`,
		"GET /app.bsky.feed.getPostThread":       `{"thread":{"post":{"uri":"at://p","likeCount":5,"repostCount":2,"replyCount":1}}}`,
	})
	orig := blueskyBaseURL
	blueskyBaseURL = srv.URL
	t.Cleanup(func() { blueskyBaseURL = orig })
	conn := func(extra map[string]interface{}) map[string]interface{} {
		c := map[string]interface{}{"credential_id": "bsky", "identifier": "me.bsky.social", "app_password": "pw"}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}

	t.Run("create_post", func(t *testing.T) {
		items := runLegacy(t, &BlueskyNode{}, conn(map[string]interface{}{"operation": "create_post", "text": "hello"}))
		sess := srv.find(t, "POST /com.atproto.server.createSession").json(t)
		if sess["identifier"] != "me.bsky.social" || sess["password"] != "pw" {
			t.Errorf("createSession body = %v", sess)
		}
		rec := srv.find(t, "POST /com.atproto.repo.createRecord")
		if rec.Auth != "Bearer jwt-1" {
			t.Errorf("Authorization = %q", rec.Auth)
		}
		body := rec.json(t)
		record, _ := body["record"].(map[string]interface{})
		if body["repo"] != "did:plc:me" || body["collection"] != "app.bsky.feed.post" || record["text"] != "hello" {
			t.Errorf("createRecord body = %v", body)
		}
		if items[0].JSON["uri"] != "at://did:plc:me/app.bsky.feed.post/1" {
			t.Errorf("output = %v", items[0].JSON)
		}
	})

	t.Run("get_post_metrics", func(t *testing.T) {
		items := runLegacy(t, &BlueskyNode{}, conn(map[string]interface{}{"operation": "get_post_metrics", "post_uri": "at://did:plc:x/app.bsky.feed.post/9"}))
		req := srv.find(t, "GET /app.bsky.feed.getPostThread")
		if req.Auth != "Bearer jwt-1" || !containsQuery(req.RawQuery, "uri", "at://did:plc:x/app.bsky.feed.post/9") {
			t.Errorf("request = %+v", req)
		}
		got := items[0].JSON
		if got["like_count"] != float64(5) || got["repost_count"] != float64(2) || got["reply_count"] != float64(1) {
			t.Errorf("metrics = %v", got)
		}
	})
}

func TestLegacyMastodonConfigs(t *testing.T) {
	srv := newLegacyServer(t, map[string]string{
		"POST /api/v1/statuses":    `{"id":"110","url":"https://x/@me/110","favourites_count":0,"reblogs_count":0}`,
		"GET /api/v1/statuses/110": `{"id":"110","url":"https://x/@me/110","favourites_count":3,"reblogs_count":1,"replies_count":2}`,
	})
	// A saved mastodon connection injects instance_url (here with a trailing
	// slash, as people paste it) and access_token.
	conn := func(extra map[string]interface{}) map[string]interface{} {
		c := map[string]interface{}{"credential_id": "masto", "instance_url": srv.URL + "/", "access_token": "tok"}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}

	t.Run("create_status", func(t *testing.T) {
		items := runLegacy(t, &MastodonNode{}, conn(map[string]interface{}{"operation": "create_status", "text": "toot", "in_reply_to_id": "99"}))
		req := srv.find(t, "POST /api/v1/statuses")
		if req.Auth != "Bearer tok" {
			t.Errorf("Authorization = %q", req.Auth)
		}
		body := req.json(t)
		if body["status"] != "toot" || body["visibility"] != "public" || body["in_reply_to_id"] != "99" {
			t.Errorf("body = %v (legacy create_status defaults visibility to public)", body)
		}
		if items[0].JSON["id"] != "110" {
			t.Errorf("output = %v", items[0].JSON)
		}
	})

	t.Run("create_status keeps explicit visibility", func(t *testing.T) {
		srv.reqs = nil
		runLegacy(t, &MastodonNode{}, conn(map[string]interface{}{"operation": "create_status", "text": "t", "visibility": "unlisted"}))
		if body := srv.find(t, "POST /api/v1/statuses").json(t); body["visibility"] != "unlisted" {
			t.Errorf("visibility = %v, want unlisted", body["visibility"])
		}
	})

	t.Run("get_status_metrics", func(t *testing.T) {
		items := runLegacy(t, &MastodonNode{}, conn(map[string]interface{}{"operation": "get_status_metrics", "status_id": "110"}))
		if req := srv.find(t, "GET /api/v1/statuses/110"); req.Auth != "Bearer tok" {
			t.Errorf("Authorization = %q", req.Auth)
		}
		got := items[0].JSON
		if got["id"] != "110" || got["url"] != "https://x/@me/110" || got["favourites_count"] != float64(3) || got["reblogs_count"] != float64(1) {
			t.Errorf("metrics = %v", got)
		}
	})
}

func TestMastodonNormalizeInstanceURL(t *testing.T) {
	cases := map[string]string{
		"https://fosstodon.org":  "https://fosstodon.org",
		"https://fosstodon.org/": "https://fosstodon.org",
		"fosstodon.org":          "https://fosstodon.org",
		" ":                      "",
	}
	for in, want := range cases {
		if got := mastodonNormalizeInstanceURL(in); got != want {
			t.Errorf("mastodonNormalizeInstanceURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLegacyRedditConfigs(t *testing.T) {
	srv := newLegacyServer(t, map[string]string{
		"POST /api/submit":  `{"json":{"errors":[],"data":{"id":"abc","name":"t3_abc","url":"https://reddit.com/r/x/comments/abc/"}}}`,
		"POST /api/comment": `{"json":{"errors":[],"data":{"things":[]}}}`,
		"GET /comments/abc": `[{"data":{"children":[]}},{"data":{"children":[{"kind":"t1","data":{"id":"c1","author":"u","body":"hi","score":4}},{"kind":"more","data":{"id":"m"}}]}}]`,
		"GET /api/info":     `{"data":{"children":[{"data":{"score":12,"num_comments":3,"permalink":"/r/x/comments/abc/"}}]}}`,
	})
	orig := redditBaseURL
	redditBaseURL = srv.URL
	t.Cleanup(func() { redditBaseURL = orig })
	conn := func(extra map[string]interface{}) map[string]interface{} {
		c := map[string]interface{}{"credential_id": "reddit", "access_token": "tok"}
		for k, v := range extra {
			c[k] = v
		}
		return c
	}

	cases := []struct {
		name   string
		config map[string]interface{}
		req    string
		check  func(t *testing.T, r recordedRequest)
	}{
		{"submit_post self", map[string]interface{}{"operation": "submit_post", "subreddit": "golang", "title": "T", "text": "body"}, "POST /api/submit",
			func(t *testing.T, r recordedRequest) {
				f := r.form(t)
				if f.Get("kind") != "self" || f.Get("sr") != "golang" || f.Get("title") != "T" || f.Get("text") != "body" || f.Get("api_type") != "json" {
					t.Errorf("form = %v", f)
				}
			}},
		{"submit_post title-only self", map[string]interface{}{"operation": "submit_post", "subreddit": "golang", "title": "T"}, "POST /api/submit",
			func(t *testing.T, r recordedRequest) {
				if f := r.form(t); f.Get("kind") != "self" || f.Get("text") != "" {
					t.Errorf("form = %v", f)
				}
			}},
		{"submit_post link without kind", map[string]interface{}{"operation": "submit_post", "subreddit": "golang", "title": "T", "url": "https://go.dev"}, "POST /api/submit",
			func(t *testing.T, r recordedRequest) {
				if f := r.form(t); f.Get("kind") != "link" || f.Get("url") != "https://go.dev" {
					t.Errorf("form = %v", f)
				}
			}},
		{"reply_to_comment bare id", map[string]interface{}{"operation": "reply_to_comment", "thing_id": "xyz", "text": "thanks"}, "POST /api/comment",
			func(t *testing.T, r recordedRequest) {
				if f := r.form(t); f.Get("thing_id") != "t1_xyz" || f.Get("text") != "thanks" || f.Get("api_type") != "json" {
					t.Errorf("form = %v", f)
				}
			}},
		{"reply_to_comment post fullname", map[string]interface{}{"operation": "reply_to_comment", "thing_id": "t3_abc", "text": "x"}, "POST /api/comment",
			func(t *testing.T, r recordedRequest) {
				if f := r.form(t); f.Get("thing_id") != "t3_abc" {
					t.Errorf("thing_id = %q", f.Get("thing_id"))
				}
			}},
		{"list_comments", map[string]interface{}{"operation": "list_comments", "thing_id": "t3_abc"}, "GET /comments/abc",
			func(t *testing.T, r recordedRequest) {
				if r.RawQuery != "raw_json=1" {
					t.Errorf("query = %q", r.RawQuery)
				}
			}},
		{"get_post_metrics bare id", map[string]interface{}{"operation": "get_post_metrics", "thing_id": "abc"}, "GET /api/info",
			func(t *testing.T, r recordedRequest) {
				if !containsQuery(r.RawQuery, "id", "t3_abc") {
					t.Errorf("query = %q", r.RawQuery)
				}
			}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv.reqs = nil
			items := runLegacy(t, &RedditNode{}, conn(tc.config))
			r := srv.find(t, tc.req)
			if r.Auth != "Bearer tok" {
				t.Errorf("Authorization = %q", r.Auth)
			}
			tc.check(t, r)
			switch tc.config["operation"] {
			case "list_comments":
				if len(items) != 1 || items[0].JSON["id"] != "c1" || items[0].JSON["score"] != 4 {
					t.Errorf("comments = %+v (the 'more' stub must be skipped)", items)
				}
			case "get_post_metrics":
				if items[0].JSON["score"] != float64(12) || items[0].JSON["num_comments"] != float64(3) || items[0].JSON["permalink"] != "/r/x/comments/abc/" {
					t.Errorf("metrics = %v", items[0].JSON)
				}
			}
		})
	}
}

// containsQuery reports whether rawQuery has key=value (decoded).
func containsQuery(rawQuery, key, value string) bool {
	v, err := url.ParseQuery(rawQuery)
	return err == nil && v.Get(key) == value
}
