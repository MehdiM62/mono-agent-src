package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestCheckRenderURLAcceptsOnlyPublicWebPages(t *testing.T) {
	for _, ok := range []string{"https://example.com/a", "http://www.producthunt.com/posts/x", "https://8.8.8.8/"} {
		if err := checkRenderURL(ok); err != nil {
			t.Errorf("%s refused: %v", ok, err)
		}
	}
	for _, bad := range []string{
		"file:///etc/passwd", "javascript:alert(1)", "ftp://example.com/",
		"http://localhost:8080/", "http://app.localhost/", "http://127.0.0.1/", "http://[::1]/",
		"http://10.0.0.5/", "http://192.168.1.1/", "http://169.254.169.254/latest/meta-data/",
		"http://0.0.0.0/", "http://intranet/", "http://printer.local/", "http://db.internal/",
	} {
		if err := checkRenderURL(bad); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}
}

func TestRenderHTMLNeedsAProfile(t *testing.T) {
	_, err := renderHTML(&globalConfig{})(context.Background(), "https://example.com/", 0)
	if err == nil || !strings.Contains(err.Error(), "profile") {
		t.Fatalf("want a no-profile error, got %v", err)
	}
}

func TestPageOuterHTMLStopsWaitingWhenCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	start := time.Now()
	if _, err := pageOuterHTML(ctx, nil, time.Minute); err == nil || time.Since(start) > time.Second {
		t.Fatalf("a cancelled wait must return at once with an error (err=%v, took %v)", err, time.Since(start))
	}
}
