package gemini

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
	"github.com/monoes/mono-agent/internal/fsconfine"
)

func (b *GeminiBot) methodTypePrompt(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "type_prompt")
	if err != nil {
		return nil, err
	}
	if len(args) < 2 {
		return nil, fmt.Errorf("type_prompt: requires (page, promptText)")
	}
	prompt, ok := args[1].(string)
	if !ok {
		return nil, fmt.Errorf("type_prompt: second arg must be string")
	}

	selectors := []string{
		"div.ql-editor[contenteditable='true']",
		"[role='textbox'][aria-label*='prompt' i]",
		"rich-textarea .ql-editor",
	}

	// Extension path: use TypeCDP (Chrome Debugger Input.insertText) which
	// reliably triggers Gemini's Quill editor — content-script paste doesn't.
	if ep, ok := page.(*extension.ExtensionPage); ok {
		var lastErr error
		for _, sel := range selectors {
			el, err := ep.Element(sel, 5*time.Second)
			if err != nil || el == nil {
				continue
			}
			ee, ok := el.(*extension.ExtensionElement)
			if !ok {
				continue
			}
			// The CDP type call's error was previously discarded here (`_ =
			// ...`), so this always reported success even when nothing was
			// actually typed — masking real failures (e.g. a stale
			// element, or CDP debugger attach rejected) and skipping the
			// JSON action definition's own tier-2/tier-3 fallback selectors,
			// which only trigger off an explicit "typeResult" failure.
			if err := ep.TypeCDPOnElement(prompt, ee.ElementID()); err != nil {
				lastErr = err
				continue
			}
			return map[string]interface{}{"success": true, "typed": len(prompt)}, nil
		}
		if lastErr != nil {
			return nil, fmt.Errorf("type_prompt: could not type into input: %w", lastErr)
		}
		return nil, fmt.Errorf("type_prompt: could not find input to type into")
	}

	// Dismiss cookie consent dialog if present (blocks all Gemini UI interactions).
	consentResult, _ := page.Eval(`() => {
		var btns = document.querySelectorAll('button');
		for (var i = 0; i < btns.length; i++) {
			var t = btns[i].textContent.trim();
			if (t === 'Accept all' || t === 'Accept') {
				btns[i].click();
				return {dismissed: true};
			}
		}
		return {dismissed: false};
	}`)
	if consentResult != nil && consentResult.Get("dismissed").Bool() {
		time.Sleep(1 * time.Second)
	}

	// Rod path: inject text into Quill's model, trigger Angular CD, then poll
	// for the send button and click it — all in one Promise-based eval.
	// CDP keyboard events and Input.insertText bypass Quill's Delta model.
	// Angular's zone.js might not be loaded (Angular 17 zoneless), so we rely
	// on polling rather than zone-based change detection.
	quillJS := fmt.Sprintf(`() => new Promise(function(resolve) {
		var btnSels = [
			"button[aria-label='Send prompt']",
			"button[aria-label='Send message']",
			"button[aria-label*='send' i]",
			"button[data-testid='send-button']"
		];
		function tryClick() {
			for (var i = 0; i < btnSels.length; i++) {
				var btn = document.querySelector(btnSels[i]);
				if (btn && !btn.disabled && btn.offsetParent !== null) {
					btn.click();
					return resolve({ok: true, method: 'quill-api', clicked: true, sel: btnSels[i]});
				}
			}
		}
		// Find Quill instance
		var container = document.querySelector('.ql-container');
		var quill = null;
		if (container) {
			quill = container.__quill;
			if (!quill && window.Quill) quill = window.Quill.find(container);
		}
		if (!quill) {
			var editor = document.querySelector('.ql-editor');
			if (editor && editor.parentElement) {
				quill = editor.parentElement.__quill;
				if (!quill && window.Quill) quill = window.Quill.find(editor.parentElement);
			}
		}
		if (!quill) {
			return resolve({ok: false, reason: 'no quill instance found'});
		}
		// Use 'user' source: Gemini's Angular text-change handler likely checks
		// source === 'user' before enabling the send button.
		quill.setText(%q, 'user');
		quill.setSelection(quill.getLength(), 0);
		// Dispatch DOM events as belt-and-suspenders for Angular change detection.
		if (quill.root) {
			quill.root.dispatchEvent(new InputEvent('input', {
				bubbles: true, cancelable: true, inputType: 'insertText', data: %q
			}));
			quill.root.dispatchEvent(new Event('change', {bubbles: true}));
		}
		// Poll for send button up to 3s (Angular CD may take a few frames)
		var attempts = 0;
		function poll() {
			attempts++;
			for (var i = 0; i < btnSels.length; i++) {
				var btn = document.querySelector(btnSels[i]);
				if (btn && !btn.disabled && btn.offsetParent !== null) {
					btn.click();
					return resolve({ok: true, method: 'quill-api', clicked: true, sel: btnSels[i], attempts: attempts});
				}
			}
			if (attempts < 15) {
				setTimeout(poll, 200);
			} else {
				resolve({ok: true, method: 'quill-api', clicked: false, reason: 'btn-not-found', attempts: attempts});
			}
		}
		setTimeout(poll, 100);
	})`, prompt, prompt)
	qr, err := page.Eval(quillJS)
	if err != nil {
		return nil, fmt.Errorf("type_prompt: eval failed: %w", err)
	}
	if qr == nil || !qr.Get("ok").Bool() {
		reason := "unknown"
		if qr != nil {
			reason = qr.Get("reason").Str()
		}
		return nil, fmt.Errorf("type_prompt: could not inject text: %s", reason)
	}
	time.Sleep(300 * time.Millisecond)
	return map[string]interface{}{"success": true, "typed": len(prompt), "sent": qr.Get("clicked").Bool()}, nil
}

func (b *GeminiBot) methodClickSend(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "click_send")
	if err != nil {
		return nil, err
	}

	// Extension path: use JS to find and click the send button, then fall back to Enter.
	if ep, ok := page.(*extension.ExtensionPage); ok {
		raw, _ := ep.EvalCDP(`(function() {
			// Try known button selectors in order of likelihood.
			var sels = [
				"button[aria-label='Send prompt']",
				"button[aria-label='Send message']",
				"button[aria-label*='send' i]",
				"button[data-testid='send-button']",
				"button.send-button",
				".send-button-container button",
				"form button[type='submit']",
				"footer button:last-of-type",
				"[class*='send'] button",
				"[class*='submit'] button"
			];
			for (var i = 0; i < sels.length; i++) {
				var el = document.querySelector(sels[i]);
				if (el && !el.disabled && el.offsetParent !== null) {
					el.click();
					return {clicked: true, sel: sels[i]};
				}
			}
			// Fallback: focus the contenteditable and dispatch Enter.
			var input = document.querySelector('.ql-editor[contenteditable="true"]') ||
				document.querySelector('[contenteditable="true"]') ||
				document.querySelector('[role="textbox"]');
			if (input) {
				input.focus();
				input.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true}));
				input.dispatchEvent(new KeyboardEvent('keyup', {key: 'Enter', code: 'Enter', keyCode: 13, which: 13, bubbles: true}));
				return {clicked: false, method: 'enter_dispatch'};
			}
			return {clicked: false, method: 'none'};
		})()`)
		if m, ok := raw.(map[string]interface{}); ok {
			if clicked, _ := m["clicked"].(bool); clicked {
				return map[string]interface{}{"success": true, "method": m["sel"]}, nil
			}
		}
		// Final fallback: press Enter via CDP keyboard.
		_ = page.KeyboardPress('\r')
		return map[string]interface{}{"success": true, "method": "enter_key"}, nil
	}

	// Rod path: try button selectors with short timeouts, then keyboard fallbacks.
	selectors := []string{
		"button[aria-label='Send prompt']",
		"button[aria-label='Send message']",
		"button[aria-label*='Send' i]",
		"button[data-testid='send-button']",
		"button.send-button",
		".send-button-container button",
		"form button[type='submit']",
	}
	for _, sel := range selectors {
		el, err := page.Element(sel, 3*time.Second)
		if err == nil && el != nil {
			_ = el.Click()
			return map[string]interface{}{"success": true, "selector": sel}, nil
		}
	}

	// Re-focus the input then try keyboard send variants.
	if el, err2 := page.Element("div.ql-editor[contenteditable='true']", 2*time.Second); err2 == nil && el != nil {
		_ = el.Click()
		time.Sleep(200 * time.Millisecond)
	}
	_ = page.KeyboardPress(rune(0x0D)) // Enter
	time.Sleep(300 * time.Millisecond)
	return map[string]interface{}{"success": true, "method": "enter_key"}, nil
}

// methodUploadImage uploads a local image file to Gemini's chat input before
// submitting a prompt. If imagePath is empty or the file does not exist it is
// a no-op (returns success) so the node still works without a reference image.
//
// Gemini's upload flow (discovered via live DOM inspection):
//  1. Click "+" button (aria-label="Open upload file menu")
//  2. Click "Upload files" menuitem (aria-label contains "Upload files")
//  3. A hidden <input type="file" name="Filedata"> appears in the DOM
//  4. Call SetFiles on that input to trigger the upload
//  5. Wait for the image thumbnail to appear in the chat input area
func (b *GeminiBot) methodUploadImage(ctx context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "upload_image")
	if err != nil {
		return nil, err
	}

	// Second arg: local image file path (may be empty — treated as no-op).
	if len(args) < 2 {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "no image path"}, nil
	}
	imagePath, _ := args[1].(string)
	imagePath = strings.TrimSpace(imagePath)
	if imagePath == "" {
		return map[string]interface{}{"success": true, "skipped": true, "reason": "empty image path"}, nil
	}
	if strings.HasPrefix(imagePath, "~/") {
		imagePath = filepath.Join(homeDir(), imagePath[2:])
	}
	// C-46: the image is a local file handed to a web page; confine it to
	// the org workdir when a role's grant started this run.
	if imagePath, err = fsconfine.Path(ctx, imagePath); err != nil {
		return nil, fmt.Errorf("upload_image: %w", err)
	}
	if _, err := os.Stat(imagePath); err != nil {
		return nil, fmt.Errorf("upload_image: image file not found: %s", imagePath)
	}

	// Step 1: Click the "+" button to open the upload menu.
	plusBtnSelectors := []string{
		"button[aria-label='Open upload file menu']",
		"button[aria-label*='upload file menu' i]",
		"button[aria-label*='Upload' i][aria-label*='menu' i]",
	}
	var plusClicked bool
	for _, sel := range plusBtnSelectors {
		el, err := page.Element(sel, 5*time.Second)
		if err == nil && el != nil {
			_ = el.Click()
			plusClicked = true
			time.Sleep(800 * time.Millisecond)
			break
		}
	}
	if !plusClicked {
		return nil, fmt.Errorf("upload_image: could not find the '+' upload menu button")
	}

	// Step 2: Click the "Upload files" menu item.
	uploadItemSelectors := []string{
		"[role='menuitem'][aria-label*='Upload files' i]",
		"[role='menuitem'][aria-label*='Upload' i]",
	}
	var uploadClicked bool
	for _, sel := range uploadItemSelectors {
		el, err := page.Element(sel, 5*time.Second)
		if err == nil && el != nil {
			_ = el.Click()
			uploadClicked = true
			time.Sleep(800 * time.Millisecond)
			break
		}
	}
	if !uploadClicked {
		return nil, fmt.Errorf("upload_image: could not find the 'Upload files' menu item")
	}

	// Step 3: Find the hidden file input and set files on it.
	fileInputSelectors := []string{
		"input[name='Filedata']",
		"input[type='file'][name='Filedata']",
		"input[type='file']",
	}

	// Extension path — uses CDP for file injection.
	if ep, ok := page.(*extension.ExtensionPage); ok {
		// Try SetFiles first (extension reads file on Go side, sends base64).
		for _, sel := range fileInputSelectors {
			el, err := ep.Element(sel, 5*time.Second)
			if err == nil && el != nil {
				if err := el.SetFiles([]string{imagePath}); err == nil {
					time.Sleep(3 * time.Second)
					return map[string]interface{}{"success": true, "uploaded": imagePath}, nil
				}
			}
		}

		// Fallback: inject file via CDP DataTransfer into the file input.
		data, err := os.ReadFile(imagePath)
		if err != nil {
			return nil, fmt.Errorf("upload_image: read file: %w", err)
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		mime := "image/jpeg"
		ext := strings.ToLower(filepath.Ext(imagePath))
		switch ext {
		case ".png":
			mime = "image/png"
		case ".gif":
			mime = "image/gif"
		case ".webp":
			mime = "image/webp"
		}
		name := filepath.Base(imagePath)

		js := fmt.Sprintf(`(function() {
			var input = document.querySelector("input[name='Filedata']") || document.querySelector("input[type='file']");
			if (!input) return {success: false, reason: "no file input found after menu click"};
			var b64 = %q;
			var mime = %q;
			var name = %q;
			var bin = atob(b64);
			var buf = new Uint8Array(bin.length);
			for (var k = 0; k < bin.length; k++) buf[k] = bin.charCodeAt(k);
			var file = new File([buf.buffer], name, {type: mime, lastModified: Date.now()});
			var dt = new DataTransfer();
			dt.items.add(file);
			Object.defineProperty(input, "files", {value: dt.files, configurable: true, writable: false});
			input.dispatchEvent(new Event("change", {bubbles: true}));
			input.dispatchEvent(new InputEvent("input", {bubbles: true}));
			return {success: true, name: name, size: file.size};
		})()`, b64, mime, name)

		raw, _ := ep.EvalCDP(js)
		if m, ok := raw.(map[string]interface{}); ok {
			if s, _ := m["success"].(bool); s {
				time.Sleep(3 * time.Second)
				return map[string]interface{}{"success": true, "uploaded": imagePath}, nil
			}
			if reason, _ := m["reason"].(string); reason != "" {
				return nil, fmt.Errorf("upload_image: CDP inject failed: %s", reason)
			}
		}
		return nil, fmt.Errorf("upload_image: could not set files via CDP")
	}

	// Rod path — standard SetFiles via file input.
	for _, sel := range fileInputSelectors {
		el, err := page.Element(sel, 5*time.Second)
		if err == nil && el != nil {
			if err := el.SetFiles([]string{imagePath}); err == nil {
				time.Sleep(3 * time.Second)
				return map[string]interface{}{"success": true, "uploaded": imagePath}, nil
			}
		}
	}

	return nil, fmt.Errorf("upload_image: could not find file input after opening upload menu")
}
