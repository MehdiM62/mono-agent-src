//go:build !nosocial

package tiktok

import (
	"context"
	"fmt"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Share / Stitch / Duet — these only open TikTok's own UI (share sheet, the
// stitch/duet creator); nothing is published.
// ---------------------------------------------------------------------------

var shareSelectors = []string{
	"[data-e2e='share-icon']",
	"[data-e2e='browse-share-icon']",
	"[data-e2e='share-btn']",
}

const jsFindOption = `
const c = all(sels).map(e => e.closest('button,[role="button"],a,li') || e).filter((b, i, a) => a.indexOf(b) === i && visible(b));
if (c.length !== 1) return {count: c.length};
mark(c[0], token);
return {count: 1, state: disabledOf(c[0]) || c[0].querySelector('[aria-disabled="true"]') ? 'disabled' : 'enabled'};
`

// clickOption opens the share sheet if needed, finds the single option
// matching sels and clicks it.
func clickOption(ctx context.Context, page browser.PageInterface, u, what string, sels []string) error {
	tok := newToken()
	defer unmark(page, tok)
	var f toggleFind
	if err := evalJS(page, "sels, token", jsFindOption, &f, shareSelectors, tok); err != nil {
		return fmt.Errorf("tiktok: find share button: %w", err)
	}
	if f.Count != 1 {
		return fmt.Errorf("tiktok: share button not found on %s (%d matches)", u, f.Count)
	}
	if err := clickMarked(page, tok); err != nil {
		return fmt.Errorf("tiktok: open share menu on %s: %w", u, err)
	}
	ok, _ := waitFor(ctx, findTimeout, func() (bool, error) {
		err := evalJS(page, "sels, token", jsFindOption, &f, sels, tok)
		return f.Count > 0, err
	})
	if !ok {
		return fmt.Errorf("tiktok: %s option not found in the share menu of %s", what, u)
	}
	if f.Count != 1 {
		return fmt.Errorf("tiktok: %d %s options in the share menu of %s", f.Count, what, u)
	}
	if f.State == "disabled" {
		return fmt.Errorf("tiktok: %s is disabled for %s (the creator does not allow it)", what, u)
	}
	return clickMarked(page, tok)
}

// StitchVideo opens the Stitch creator for the video. Status
// "opened_stitch_editor": the stitch itself is not recorded or published.
func (b *TikTokBot) StitchVideo(ctx context.Context, page browser.PageInterface, videoURL string) (map[string]interface{}, error) {
	u, err := requireVideoURL(videoURL)
	if err != nil {
		return nil, err
	}
	if err := open(ctx, page, u); err != nil {
		return nil, err
	}
	if err := clickOption(ctx, page, u, "Stitch", []string{"[data-e2e='share-stitch']"}); err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "status": "opened_stitch_editor", "published": false, "videoURL": u}, nil
}

// DuetVideo opens the Duet creator for the video. Status
// "opened_duet_editor": the duet itself is not recorded or published.
func (b *TikTokBot) DuetVideo(ctx context.Context, page browser.PageInterface, videoURL string) (map[string]interface{}, error) {
	u, err := requireVideoURL(videoURL)
	if err != nil {
		return nil, err
	}
	if err := open(ctx, page, u); err != nil {
		return nil, err
	}
	if err := clickOption(ctx, page, u, "Duet", []string{"[data-e2e='share-duet']"}); err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "status": "opened_duet_editor", "published": false, "videoURL": u}, nil
}

// ShareVideo opens the share menu and clicks "Copy link". Nothing is posted;
// the result carries the video URL.
func (b *TikTokBot) ShareVideo(ctx context.Context, page browser.PageInterface, videoURL string) (map[string]interface{}, error) {
	u, err := requireVideoURL(videoURL)
	if err != nil {
		return nil, err
	}
	if err := open(ctx, page, u); err != nil {
		return nil, err
	}
	if err := clickOption(ctx, page, u, "Copy link", []string{"[data-e2e='copy-link-icon']", "[data-e2e='copy-link']"}); err != nil {
		return nil, err
	}
	return map[string]interface{}{"success": true, "status": "link_copied", "published": false, "url": u, "videoURL": u}, nil
}
