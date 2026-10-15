package gemini

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/browser"

	botpkg "github.com/monoes/mono-agent/internal/bot"
)

// homeDir returns the current user's home directory, falling back to "" if
// it can't be resolved. Unlike the raw HOME env var, os.UserHomeDir also
// checks USERPROFILE on Windows.
func homeDir() string {
	home, _ := os.UserHomeDir()
	return home
}

// GeminiBot implements botpkg.BotAdapter for Google Gemini.
type GeminiBot struct {
	// downloadedImages holds the content hashes of images this bot has already
	// downloaded. Image extraction reads every image in the open conversation,
	// so in a continued session (several prompts in one chat) each extraction
	// would otherwise re-download and re-vault every earlier turn's image.
	downloadedImages map[string]bool
}

func init() {
	botpkg.PlatformRegistry["GEMINI"] = func() botpkg.BotAdapter {
		return &GeminiBot{}
	}
}

// Platform returns the canonical platform name.
func (b *GeminiBot) Platform() string { return "GEMINI" }

// LoginURL returns the Gemini login page URL.
func (b *GeminiBot) LoginURL() string { return "https://gemini.google.com" }

// IsLoggedIn checks whether the user is authenticated on Gemini by looking
// for sign-in buttons (NOT logged in) and prompt input (IS logged in).
func (b *GeminiBot) IsLoggedIn(page browser.PageInterface) (bool, error) {
	// Check for sign-in button — if present, NOT logged in.
	signInSelectors := []string{
		"a[href*='accounts.google.com/ServiceLogin']",
		"a[href*='accounts.google.com/signin']",
		"button[data-signin]",
	}
	for _, sel := range signInSelectors {
		has, err := page.Has(sel)
		if err != nil {
			continue
		}
		if has {
			return false, nil
		}
	}

	// Check for prompt input — only appears when logged in.
	promptSelectors := []string{
		"div.ql-editor[contenteditable='true']",
		"rich-textarea",
		"input-area-v2",
	}
	for _, sel := range promptSelectors {
		has, err := page.Has(sel)
		if err != nil {
			continue
		}
		if has {
			return true, nil
		}
	}

	return false, nil
}

// ResolveURL returns the Gemini base URL for any input.
func (b *GeminiBot) ResolveURL(_ string) string {
	return "https://gemini.google.com"
}

// ExtractUsername returns a placeholder — Gemini has no user profiles.
func (b *GeminiBot) ExtractUsername(_ string) string {
	return "gemini-user"
}

// SearchURL is not applicable for Gemini.
func (b *GeminiBot) SearchURL(_ string) string {
	return ""
}

// SendMessage is not supported for Gemini.
func (b *GeminiBot) SendMessage(_ context.Context, _ browser.PageInterface, _, _ string) error {
	return fmt.Errorf("gemini: SendMessage not supported")
}

// GetProfileData is not supported for Gemini.
func (b *GeminiBot) GetProfileData(_ context.Context, _ browser.PageInterface) (map[string]interface{}, error) {
	return nil, fmt.Errorf("gemini: GetProfileData not supported")
}

// GetMethodByName returns a dispatchable wrapper for the named Gemini action
// method. The executor calls this to resolve call_bot_method steps.
func (b *GeminiBot) GetMethodByName(name string) (func(ctx context.Context, args ...interface{}) (interface{}, error), bool) {
	switch name {
	case "find_prompt_input":
		return b.methodFindPromptInput, true
	case "type_prompt":
		return b.methodTypePrompt, true
	case "click_send":
		return b.methodClickSend, true
	case "wait_for_response":
		return b.methodWaitForResponse, true
	case "wait_for_image_response":
		return b.methodWaitForImageResponse, true
	case "extract_text_response":
		return b.methodExtractTextResponse, true
	case "extract_image_response":
		return b.methodExtractImageResponse, true
	case "download_images":
		return b.methodDownloadImages, true
	case "extract_and_download_images":
		return b.methodExtractAndDownloadImages, true
	case "upload_image":
		return b.methodUploadImage, true
	case "navigate_to_session":
		return b.methodNavigateToSession, true
	case "get_session_id":
		return b.methodGetSessionID, true
	case "wait_for_any_response":
		return b.methodWaitForAnyResponse, true
	case "extract_response_by_mode":
		return b.methodExtractResponseByMode, true
	default:
		return nil, false
	}
}

// ---------------------------------------------------------------------------
// Bot methods — all use browser.PageInterface for extension compatibility
// ---------------------------------------------------------------------------

// extractPage gets browser.PageInterface from args[0], accepting both
// PageInterface directly and *rod.Page (wrapped automatically).
func extractPage(args []interface{}, methodName string) (browser.PageInterface, error) {
	if len(args) < 1 {
		return nil, fmt.Errorf("%s: requires (page)", methodName)
	}
	if p, ok := args[0].(browser.PageInterface); ok {
		return p, nil
	}
	return nil, fmt.Errorf("%s: first arg must be browser.PageInterface, got %T", methodName, args[0])
}

func (b *GeminiBot) methodFindPromptInput(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "find_prompt_input")
	if err != nil {
		return nil, err
	}
	selectors := []string{
		"div.ql-editor[contenteditable='true']",
		"[role='textbox'][aria-label*='prompt' i]",
		"rich-textarea .ql-editor",
		"rich-textarea",
		"[contenteditable='true'][data-placeholder]",
	}
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		for _, sel := range selectors {
			el, err := page.Element(sel, 2*time.Second)
			if err == nil && el != nil {
				return map[string]interface{}{"success": true, "selector": sel}, nil
			}
		}
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("find_prompt_input: could not find prompt input after 20s")
}

// methodNavigateToSession navigates to an existing Gemini session by session ID.
// If session_id is empty or missing, it is a no-op (caller already navigated to /app).
func (b *GeminiBot) methodNavigateToSession(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "navigate_to_session")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "no session_id"}, nil
	}
	sessionID, _ := args[1].(string)
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "empty session_id"}, nil
	}
	targetURL := "https://gemini.google.com/app/" + sessionID
	if err := page.Navigate(targetURL); err != nil {
		return nil, fmt.Errorf("navigate_to_session: %w", err)
	}
	time.Sleep(4 * time.Second)
	return map[string]interface{}{"success": true, "url": targetURL}, nil
}

// methodGetSessionID extracts the Gemini session ID from the current page URL.
// URL format: https://gemini.google.com/app/{sessionID}
func (b *GeminiBot) methodGetSessionID(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "get_session_id")
	if err != nil {
		return nil, err
	}
	currentURL, err := page.GetURL()
	if err != nil {
		return nil, fmt.Errorf("get_session_id: could not get page URL: %w", err)
	}
	u, err := url.Parse(currentURL)
	if err != nil {
		return nil, fmt.Errorf("get_session_id: invalid URL %q: %w", currentURL, err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	// path is "app/{sessionID}"
	if len(parts) >= 2 && parts[0] == "app" && parts[1] != "" {
		return map[string]interface{}{"success": true, "session_id": parts[1]}, nil
	}
	return nil, fmt.Errorf("get_session_id: no session ID in URL: %s", currentURL)
}

// methodWaitForAnyResponse dispatches to the text or image wait method based on mode.
// args: page, mode ("text"|"image"), maxWaitSeconds (optional)
func (b *GeminiBot) methodWaitForAnyResponse(ctx context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "wait_for_any_response")
	if err != nil {
		return nil, err
	}
	mode := "text"
	if len(args) >= 2 {
		if m, ok := args[1].(string); ok && m != "" {
			mode = strings.ToLower(m)
		}
	}
	delegateArgs := []interface{}{page}
	if len(args) >= 3 {
		delegateArgs = append(delegateArgs, args[2])
	}
	if mode == "image" {
		return b.methodWaitForImageResponse(ctx, delegateArgs...)
	}
	return b.methodWaitForResponse(ctx, delegateArgs...)
}

// methodExtractResponseByMode dispatches to text or image extraction based on mode.
// args: page, mode ("text"|"image"), downloadDir (optional, image mode only)
func (b *GeminiBot) methodExtractResponseByMode(ctx context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "extract_response_by_mode")
	if err != nil {
		return nil, err
	}
	mode := "text"
	if len(args) >= 2 {
		if m, ok := args[1].(string); ok && m != "" {
			mode = strings.ToLower(m)
		}
	}
	if mode == "image" {
		delegateArgs := []interface{}{page}
		if len(args) >= 3 {
			delegateArgs = append(delegateArgs, args[2])
		}
		return b.methodExtractAndDownloadImages(ctx, delegateArgs...)
	}
	return b.methodExtractTextResponse(ctx, page)
}
