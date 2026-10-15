package gemini

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/extension"
)

func (b *GeminiBot) methodWaitForResponse(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "wait_for_response")
	if err != nil {
		return nil, err
	}
	maxWait := 60
	if len(args) >= 2 {
		switch v := args[1].(type) {
		case string:
			fmt.Sscanf(v, "%d", &maxWait)
		case float64:
			maxWait = int(v)
		case int:
			maxWait = v
		}
	}

	deadline := time.Now().Add(time.Duration(maxWait) * time.Second)

	// Snapshot pre-send state.
	type preState struct {
		url         string
		textLen     int
		beforeCount int
	}
	var pre preState

	// pollJS checks for response content using three strategies.
	// Relies on stability (3 consecutive stable polls = 6s) to avoid false positives.
	pollJS := func(initURL string, initLen, beforeCnt int) string {
		return fmt.Sprintf(`(function(initURL, initLen, beforeCount) {
			// Strategy 1: named response elements (count increased + text present).
			var nameSels = [
				'message-content', 'model-response', 'ms-chat-turn',
				'response-container', 'chat-turn', 'model-turn',
				'[data-message-author-role="model"]'
			];
			for (var i = 0; i < nameSels.length; i++) {
				try {
					var els = document.querySelectorAll(nameSels[i]);
					if (els.length > beforeCount) {
						var t = (els[els.length-1].textContent || '').trim();
						if (t && t.length > 2) return {ready: true, text: t, method: 'element'};
					}
				} catch(e) {}
			}
			// Conversation area text.
			var convSels = ['chat-window-content', 'assistant-messages-primary', 'chat-message-list'];
			var chatText = '';
			for (var j = 0; j < convSels.length; j++) {
				var el = document.querySelector(convSels[j]);
				if (el) { chatText = (el.textContent || '').trim(); break; }
			}
			// Guard: page still in zero/start state → not ready regardless.
			var nowZeroState = !!(document.querySelector('modular-zero-state') || document.querySelector('zero-state-banners'));
			if (nowZeroState) { return {ready: false, text: '', method: 'zero_blocked', debug: 'isZero=true chatLen=' + chatText.length}; }
			// Strategy 2: URL changed (first message in new session from zero-state).
			if (location.href !== initURL && chatText.length > 5) {
				return {ready: true, text: chatText.substring(0, 2000), method: 'url_change'};
			}
			// Strategy 3: conversation content changed by ANY amount vs baseline.
			// Handles both zero-state→session (URL already changed at capture) and
			// continued sessions (new turn added). Stability check prevents early exit.
			if (chatText.length > 5 && Math.abs(chatText.length - initLen) >= 1) {
				return {ready: true, text: chatText.substring(0, 2000), method: 'content_change'};
			}
			return {ready: false, text: ''};
		})(%q, %d, %d)`, initURL, initLen, beforeCnt)
	}

	// Capture baseline state at the start of wait (right after click_send).
	captureJS := `(function() {
		var nameSels = ['message-content','model-response','ms-chat-turn','response-container','chat-turn'];
		var cnt = 0;
		for (var i = 0; i < nameSels.length; i++) {
			var n = document.querySelectorAll(nameSels[i]).length;
			if (n > cnt) cnt = n;
		}
		var convSels = ['chat-window-content','assistant-messages-primary'];
		var textLen = 0;
		for (var j = 0; j < convSels.length; j++) {
			var el = document.querySelector(convSels[j]);
			if (el) { textLen = (el.textContent||'').trim().length; break; }
		}
		return {url: location.href, textLen: textLen, cnt: cnt};
	})()`

	evalCapture := func() {
		if ep, ok := page.(*extension.ExtensionPage); ok {
			raw, _ := ep.EvalCDP(captureJS)
			if m, ok := raw.(map[string]interface{}); ok {
				pre.url, _ = m["url"].(string)
				if l, ok := m["textLen"].(float64); ok {
					pre.textLen = int(l)
				}
				if c, ok := m["cnt"].(float64); ok {
					pre.beforeCount = int(c)
				}
			}
		} else {
			r, _ := page.Eval(`() => ` + captureJS)
			if r != nil && !r.Nil() {
				pre.url = r.Get("url").Str()
				pre.textLen = r.Get("textLen").Int()
				pre.beforeCount = r.Get("cnt").Int()
			}
		}
	}
	evalCapture()

	// Build poll JS once — pre doesn't change after capture.
	js := pollJS(pre.url, pre.textLen, pre.beforeCount)
	_ = pre.url // used in pollJS

	prevText := ""
	stableCount := 0
	pollN := 0

	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		pollN++

		var ready bool
		var text string

		if ep, ok := page.(*extension.ExtensionPage); ok {
			raw, evalErr := ep.EvalCDP(js)
			if evalErr == nil {
				if m, ok := raw.(map[string]interface{}); ok {
					if r, _ := m["ready"].(bool); r {
						ready = true
						text, _ = m["text"].(string)
					}
				}
			}
		} else {
			result, evalErr := page.Eval(`() => ` + js)
			if evalErr != nil {
				continue
			}
			ready = result.Get("ready").Bool()
			text = result.Get("text").Str()
		}

		if !ready || text == "" {
			continue
		}
		if text == prevText {
			stableCount++
			// 3 consecutive stable polls = 6s of no text change → response complete.
			if stableCount >= 3 {
				return map[string]interface{}{"success": true, "ready": true}, nil
			}
		} else {
			stableCount = 0
		}
		prevText = text
	}
	return nil, fmt.Errorf("wait_for_response: timed out after %ds", maxWait)
}

func (b *GeminiBot) methodWaitForImageResponse(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "wait_for_image_response")
	if err != nil {
		return nil, err
	}
	maxWait := 120
	if len(args) >= 2 {
		switch v := args[1].(type) {
		case string:
			fmt.Sscanf(v, "%d", &maxWait)
		case float64:
			maxWait = int(v)
		case int:
			maxWait = v
		}
	}

	deadline := time.Now().Add(time.Duration(maxWait) * time.Second)

	if ep, ok := page.(*extension.ExtensionPage); ok {
		// Extension path: use EvalCDP (page main world, bypasses CSP).
		for time.Now().Before(deadline) {
			time.Sleep(3 * time.Second)
			raw, _ := ep.EvalCDP(`(function() {
				var containers = document.querySelectorAll('model-response, message-content, .response-container');
				if (!containers.length) return {imgCount: 0, text: ''};
				var last = containers[containers.length - 1];
				var imgs = last.querySelectorAll('img');
				var valid = 0;
				for (var i = 0; i < imgs.length; i++) {
					var img = imgs[i];
					var w = img.width || img.naturalWidth || 0;
					if (w > 0 && w < 48) continue;
					var src = img.src || '';
					if (src.indexOf('blob:') === 0 || src.indexOf('data:image') === 0 || (src.indexOf('https://') === 0 && w >= 100)) valid++;
				}
				var text = '';
				var textSels = ['message-content', 'model-response'];
				for (var j = 0; j < textSels.length; j++) {
					var els = document.querySelectorAll(textSels[j]);
					if (els.length) { text = (els[els.length-1].textContent || '').trim(); break; }
				}
				// Those container tags are frequently stale against Gemini's
				// current markup (they can legitimately match zero elements
				// even after a real reply), which silently blinds the refusal
				// check below. Fall back to the whole visible page text so a
				// refusal ("can't create", "sign in", etc.) is still caught
				// instead of surfacing only as an uninformative timeout.
				if (!text) {
					text = (document.body.innerText || '').trim();
				}
				return {imgCount: valid, text: text.substring(0, 600)};
			})()`)
			var imgCount int
			var text string
			if m, ok := raw.(map[string]interface{}); ok {
				if c, ok := m["imgCount"].(float64); ok {
					imgCount = int(c)
				}
				if t, ok := m["text"].(string); ok {
					text = t
				}
			}
			if imgCount > 0 {
				time.Sleep(3 * time.Second)
				return map[string]interface{}{"success": true, "ready": true}, nil
			}
			if text != "" {
				lower := strings.ToLower(text)
				if strings.Contains(lower, "can't create") || strings.Contains(lower, "image creation isn't available") {
					return nil, fmt.Errorf("wait_for_image_response: Gemini refused: %s", text[:min(len(text), 200)])
				}
			}
		}
		return nil, fmt.Errorf("wait_for_image_response: timed out after %ds", maxWait)
	}

	// Rod path: use Eval
	for time.Now().Before(deadline) {
		time.Sleep(3 * time.Second)
		result, err := page.Eval(`() => {
			const containers = document.querySelectorAll('model-response, message-content, .response-container');
			let text = '';
			let valid = 0;
			if (containers.length > 0) {
				const last = containers[containers.length - 1];
				const imgs = last.querySelectorAll('img');
				for (const img of imgs) {
					const w = img.width || img.naturalWidth || 0;
					if (w > 0 && w < 48) continue;
					const src = img.src || '';
					if (src.startsWith('blob:') || src.startsWith('data:image') || (src.startsWith('https://') && w >= 100)) valid++;
				}
				text = (last.textContent || '').trim();
			}
			// See the extension-path comment above: these container tags can
			// legitimately match nothing even after a real reply, which
			// blinds the refusal check below unless we fall back to the
			// whole visible page text.
			if (!text) {
				text = (document.body.innerText || '').trim();
			}
			return {found: valid > 0, text: text.substring(0, 600)};
		}`)
		if err != nil {
			continue
		}
		found := result.Get("found").Bool()
		text := result.Get("text").Str()
		if found {
			time.Sleep(3 * time.Second)
			return map[string]interface{}{"success": true, "ready": true}, nil
		}
		if text != "" {
			lower := strings.ToLower(text)
			if strings.Contains(lower, "can't create") || strings.Contains(lower, "image creation isn't available") {
				return nil, fmt.Errorf("wait_for_image_response: Gemini refused: %s", text[:min(len(text), 200)])
			}
		}
	}
	return nil, fmt.Errorf("wait_for_image_response: timed out after %ds", maxWait)
}

func (b *GeminiBot) methodExtractTextResponse(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "extract_text_response")
	if err != nil {
		return nil, err
	}

	const extractJS = `(function() {
		// Strategy 1: named response elements (works when Gemini uses them).
		var sels = [
			'message-content div.markdown.markdown-main-panel',
			'message-content',
			'model-response',
			'ms-chat-turn model-response',
			'ms-chat-turn',
			'response-container',
			'chat-turn model-response',
			'structured-content-container.model-response-text',
			'[data-message-author-role="model"]',
			'[class*="model-response"]',
			'[class*="response-text"]'
		];
		for (var i = 0; i < sels.length; i++) {
			try {
				var els = document.querySelectorAll(sels[i]);
				if (!els.length) continue;
				var text = (els[els.length - 1].textContent || '').trim();
				if (text && text.length > 2) return text;
			} catch(e) {}
		}
		// Strategy 2: grab text from the conversation content container.
		var convSels = ['chat-window-content', 'assistant-messages-primary', 'chat-message-list'];
		for (var j = 0; j < convSels.length; j++) {
			var conv = document.querySelector(convSels[j]);
			if (!conv) continue;
			var children = Array.from(conv.children || []);
			// Walk children backwards: skip the LAST child if it looks like a user turn
			// (user turns often contain input-area-v2, rich-textarea, or are very short).
			// The model's response is the most recent NON-user child with substantial text.
			var lastModelText = '';
			for (var k = children.length - 1; k >= 0; k--) {
				var child = children[k];
				// Skip if this child contains input elements (= user-input area).
				if (child.querySelector && child.querySelector('rich-textarea, input-area-v2, [contenteditable="true"]')) continue;
				var t = (child.textContent || '').trim();
				if (t && t.length > 5) { lastModelText = t; break; }
			}
			if (lastModelText) return lastModelText.substring(0, 5000);
			// Fallback: return all conversation text.
			var allText = conv.textContent.trim();
			if (allText.length > 5) return allText.substring(0, 5000);
		}
		return '';
	})()`

	if ep, ok := page.(*extension.ExtensionPage); ok {
		raw, err := ep.EvalCDP(extractJS)
		if err == nil {
			if text, ok := raw.(string); ok && text != "" {
				return map[string]interface{}{"success": true, "response_text": text}, nil
			}
		}
		return nil, fmt.Errorf("extract_text_response: no response text found")
	}

	// Rod path: use Eval
	result, err := page.Eval(`() => ` + extractJS)
	if err != nil {
		return nil, fmt.Errorf("extract_text_response: eval failed: %w", err)
	}
	text := result.Str()
	if text == "" {
		return nil, fmt.Errorf("extract_text_response: no response text found")
	}
	return map[string]interface{}{"success": true, "response_text": text}, nil
}

func (b *GeminiBot) methodExtractImageResponse(_ context.Context, args ...interface{}) (interface{}, error) {
	page, err := extractPage(args, "extract_image_response")
	if err != nil {
		return nil, err
	}
	result, err := page.Eval(`() => {
		const containers = document.querySelectorAll('model-response, message-content, .response-container');
		if (containers.length === 0) return [];
		const last = containers[containers.length - 1];
		const urls = [];
		for (const img of last.querySelectorAll('img')) {
			const src = img.src || '';
			const w = img.width || img.naturalWidth || 0;
			if (w > 0 && w < 48) continue;
			if (src.startsWith('blob:') || src.startsWith('data:image') || (src.startsWith('https://') && w >= 100)) urls.push(src);
		}
		return urls;
	}`)
	if err != nil {
		return nil, fmt.Errorf("extract_image_response: eval failed: %w", err)
	}
	var urls []string
	for _, item := range result.Arr() {
		if s := item.Str(); s != "" {
			urls = append(urls, s)
		}
	}
	if len(urls) == 0 {
		return nil, fmt.Errorf("extract_image_response: no images found")
	}
	return map[string]interface{}{"success": true, "image_urls": urls}, nil
}
