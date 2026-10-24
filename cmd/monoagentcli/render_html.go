package main

import (
	"context"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"time"

	browserpkg "github.com/monoes/mono-agent/internal/browser"
	"github.com/monoes/mono-agent/internal/logging"
	"github.com/monoes/mono-agent/internal/vault"
)

// renderHTML opens rawURL in the user's browser through the extension, waits
// wait for the page's scripts to run and returns the rendered HTML. There is
// no browser of its own to fall back on: without the extension it fails.
func renderHTML(cfg *globalConfig) func(context.Context, string, time.Duration) (string, error) {
	return func(ctx context.Context, rawURL string, wait time.Duration) (string, error) {
		return renderHTMLAs(vault.ContextWithProfileID(ctx, browserProfile(cfg)), rawURL, wait)
	}
}

func renderHTMLAs(ctx context.Context, rawURL string, wait time.Duration) (string, error) {
	if err := checkRenderURL(rawURL); err != nil {
		return "", err
	}
	profile, err := vault.ProfileIDFromContext(ctx)
	if err != nil {
		return "", err
	}
	logger := logging.Zerolog().With().Str("component", "render-html").Logger()
	bridge := setupProfileBridge(profile, logger, 3*time.Second)
	if !bridge.IsConnected() {
		if err := ensureExtensionConnected(bridge, 30*time.Second); err != nil {
			return "", fmt.Errorf("browser bridge not connected: %w", err)
		}
	}
	tabID, err := bridge.CreateTab(rawURL)
	if err != nil {
		return "", fmt.Errorf("open the page in the extension: %w", err)
	}
	defer bridge.CloseTab(tabID)
	page := bridge.NewPage(tabID)
	return pageOuterHTML(ctx, page, wait)
}

// checkRenderURL accepts plain web pages only. The page opens in the user's
// own browser, with their cookies, so file:// and javascript: are out, and so
// are the user's own machine and network (localhost, private and link-local
// addresses, single-label and .local/.internal names), which an agent could
// otherwise read through it.
func checkRenderURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %w", rawURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme %q — only http and https pages can be captured", parsed.Scheme)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if ip, err := netip.ParseAddr(host); err == nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
			return fmt.Errorf("%s is a local or private address; only public web pages can be captured", host)
		}
		return nil
	}
	if host == "" || !strings.Contains(host, ".") || host == "localhost" || strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("%q is a local or private name; only public web pages can be captured", host)
	}
	return nil
}

func pageOuterHTML(ctx context.Context, page browserpkg.PageInterface, wait time.Duration) (string, error) {
	select {
	case <-time.After(wait):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	res, err := page.Eval("() => document.documentElement.outerHTML")
	if err != nil {
		return "", fmt.Errorf("capture HTML: %w", err)
	}
	return res.Str(), nil
}
