package nodes

import (
	"strings"

	"github.com/monoes/mono-agent/internal/action"
	"github.com/monoes/mono-agent/internal/workflow"
)

// declaredOutputs collects an action's outputs.success values after a run:
// each name is a variable (e.g. extract_text's variable_name) or, failing
// that, a step id whose result data is used. Only declarative packages (no
// requires.native) get this: native built-ins keep their outputs exactly as
// they were. Returns nil when nothing resolved.
func declaredOutputs(ex *action.ActionExecutor, automationID, actionType string) map[string]interface{} {
	pkg := ex.Package()
	if pkg == nil || nativeOf(pkg) != "" {
		return nil
	}
	def, err := action.GetLoader().Load(automationID, actionType)
	if err != nil || def == nil {
		return nil
	}
	out := map[string]interface{}{}
	for _, name := range def.Outputs["success"] {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		v := ex.Resolver().ResolvePath(name)
		if v == nil {
			if sr := ex.ExecContext().GetStepResult(name); sr != nil && sr.Data != nil {
				v = sr.Data
			}
		}
		if v != nil {
			out[name] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// nativeOf is the package's requires.native ("" for declarative packages).
func nativeOf(pkg action.PackageContext) string {
	if n, ok := pkg.(interface{ Native() string }); ok {
		return n.Native()
	}
	if m, ok := bootedManifest(pkg.ID()); ok {
		return m.Requires.Native
	}
	return ""
}

// withDeclaredOutputs merges declared outputs over a copy of the input item.
func withDeclaredOutputs(inputJSON, declared map[string]interface{}) map[string]interface{} {
	item := make(map[string]interface{}, len(inputJSON)+len(declared))
	for k, v := range inputJSON {
		item[k] = v
	}
	for k, v := range declared {
		item[k] = v
	}
	return item
}

// identityKeys name who or what a record is about; their presence means the
// record's "text" is content, not a profile card. The content keys cover the
// results of write actions (a TikTok comment or engagement result carries
// videoURL and the text it posted; X/Hacker News replies carry postURL or
// itemID), whose text is never a name. Plain "url" is left out: a profile
// card may carry one.
var identityKeys = []string{"full_name", "name", "username", "author", "author_username",
	"author_name", "handle", "displayName", "display_name", "title", "author_url", "comment_id", "post_url",
	"videoURL", "video_url", "postURL", "postUrl", "itemID", "item_id", "commentID"}

func hasIdentityFields(raw map[string]interface{}) bool {
	for _, k := range identityKeys {
		if v, ok := raw[k]; ok && v != nil && v != "" {
			return true
		}
	}
	return false
}

// recordItems turns each extracted record into its own output item,
// normalised like a merged item and with step bookkeeping removed.
func recordItems(extracted []map[string]interface{}, platform string) []workflow.Item {
	items := make([]workflow.Item, 0, len(extracted))
	for _, raw := range extracted {
		if rec := OutputRecord(raw, platform); rec != nil {
			items = append(items, workflow.NewItem(rec))
		}
	}
	return items
}

// OutputRecord is the output item a browser node emits for one extracted
// record: normalised (NormalizeBrowserItem) and without step bookkeeping.
// It is nil for a record a step marked skipped.
func OutputRecord(raw map[string]interface{}, platform string) map[string]interface{} {
	if skipped, _ := raw["skipped"].(bool); skipped {
		return nil
	}
	rec := make(map[string]interface{}, len(raw)+4)
	for k, v := range NormalizeBrowserItem(raw, platform) {
		if !stepBookkeepingKeys[k] {
			rec[k] = v
		}
	}
	return rec
}

// mergeStepResults folds the input item and every step's extracted result
// into the single item a downstream node sees (rather than fanning out "3
// items for 3 steps"). Later steps win for any key they share.
//
// A step's own bookkeeping never becomes the node's answer. A no-op step —
// navigating to a session id that wasn't supplied, uploading a reference
// image that wasn't set — reports {"success":true,"skipped":true,"reason":…},
// and merging that verbatim is how a Gemini run that saved a 1MB image
// announced itself as "skipped":true,"reason":"empty session_id" next to
// "image_count":1. That is also what made the same node, on a run that
// genuinely did nothing, look like it had merely skipped a step.
func mergeStepResults(inputJSON map[string]interface{}, extracted []map[string]interface{}, platform string) map[string]interface{} {
	merged := make(map[string]interface{}, len(inputJSON)+8)
	for k, v := range inputJSON {
		merged[k] = v
	}
	for _, raw := range extracted {
		if skipped, _ := raw["skipped"].(bool); skipped {
			continue
		}
		for k, v := range NormalizeBrowserItem(raw, platform) {
			if stepBookkeepingKeys[k] {
				continue
			}
			// Don't overwrite an existing social profile platform with "gemini"
			if k == "platform" && strings.ToLower(platform) == "gemini" {
				if existingP, ok := inputJSON["platform"].(string); ok && existingP != "" {
					continue
				}
			}
			merged[k] = v
		}
	}
	return merged
}

// stepBookkeepingKeys are fields a bot step reports about *how* it worked —
// which selector matched, how many characters it typed, whether a response had
// rendered — rather than what the node produced. They belong in logs, and as a
// node's output they are noise at best: a Gemini run that saved an image
// presented "skipped":true and "reason":"empty session_id" alongside
// "image_count":1, because a no-op step's map was merged in wholesale.
//
// "success" is deliberately absent: it is bookkeeping too, but a workflow may
// already branch on it, so it keeps surfacing.
var stepBookkeepingKeys = map[string]bool{
	"selector": true,
	"method":   true,
	"typed":    true,
	"ready":    true,
	"skipped":  true,
	"reason":   true,
}

// NormalizeBrowserItem enriches a raw extracted item with structured fields.
//
// stepExtractMultiple produces items with generic keys: "text" (visible text of
// the DOM element) and "href" (the element's href attribute if present).
// This function maps those to the canonical people fields that people.save
// and SaveExtractedData expect. Exported so cmd/monoagentcli's CLI `run`
// path can apply the same normalization stepExtractMultiple's workflow-node
// counterpart (BrowserNode.Execute, above) already does — the CLI path
// previously passed raw text/href items straight to SaveExtractedData,
// which silently skipped every row for lacking a resolvable username.
func NormalizeBrowserItem(raw map[string]interface{}, platform string) map[string]interface{} {
	out := make(map[string]interface{}, len(raw)+4)
	for k, v := range raw {
		out[k] = v
	}

	// Canonical platform field (lowercase).
	if _, exists := out["platform"]; !exists {
		out["platform"] = strings.ToLower(platform)
	}

	// Map href → profile_url (and url for SaveExtractedData compatibility).
	if href, ok := raw["href"].(string); ok && href != "" {
		if _, exists := out["profile_url"]; !exists {
			out["profile_url"] = href
		}
		if _, exists := out["url"]; !exists {
			out["url"] = href
		}
	}

	// Split text into full_name and job_title.
	// LinkedIn result cards include noise lines before the actual job title
	// (e.g. "View X's profile", "• 2nd", "2nd degree connection").
	// Scan past those to find the real professional headline.
	// Only bare profile cards (a text blob and a link) get their text split
	// into name fields. A record that already says who it is about — a
	// comment's author, a post's username — keeps its text as text: a
	// comment body is not a person's name.
	if text, ok := raw["text"].(string); ok && text != "" && !hasIdentityFields(raw) {
		trimmedText := strings.TrimSpace(text)

		// Check if text is a single-line or bullet-separated LinkedIn card
		if strings.Contains(trimmedText, "•") {
			parts := strings.SplitN(trimmedText, "•", 2)
			rawName := strings.TrimSpace(parts[0])
			// Deduplicate repeated name words (e.g. "Ravinder Singh Ravinder Singh")
			words := strings.Fields(rawName)
			if len(words) >= 2 && len(words)%2 == 0 {
				half := len(words) / 2
				if strings.EqualFold(strings.Join(words[:half], " "), strings.Join(words[half:], " ")) {
					rawName = strings.Join(words[:half], " ")
				}
			}
			if _, exists := out["full_name"]; !exists && rawName != "" {
				out["full_name"] = rawName
			}
			if _, exists := out["name"]; !exists && rawName != "" {
				out["name"] = rawName
			}

			if len(parts) > 1 {
				afterBullet := strings.TrimSpace(parts[1])
				// Strip leading degree indicators e.g. "1st", "2nd", "3rd"
				for _, deg := range []string{"1st", "2nd", "3rd"} {
					if strings.HasPrefix(afterBullet, deg) {
						afterBullet = strings.TrimSpace(strings.TrimPrefix(afterBullet, deg))
						break
					}
				}

				// Find boundaries for headline (Connect, Follow, Message, Current:, Past:)
				headline := afterBullet
				cutKeywords := []string{"Connect", "Follow", "Message", "Current:", "Past:"}
				for _, kw := range cutKeywords {
					if idx := strings.Index(headline, kw); idx != -1 {
						headline = strings.TrimSpace(headline[:idx])
					}
				}
				// Also check if headline ends with a location like "Berlin..."
				lowerH := strings.ToLower(headline)
				if idx := strings.Index(lowerH, "berlin"); idx != -1 {
					headline = strings.TrimSpace(headline[:idx])
				}
				headline = strings.TrimSuffix(strings.TrimSpace(headline), "|")
				headline = strings.TrimSpace(headline)

				if _, exists := out["job_title"]; !exists && headline != "" {
					out["job_title"] = headline
				}
				if _, exists := out["headline"]; !exists && headline != "" {
					out["headline"] = headline
				}
			}
		}

		lines := strings.Split(trimmedText, "\n")
		if _, exists := out["full_name"]; !exists && len(lines) > 0 {
			out["full_name"] = strings.TrimSpace(lines[0])
		}
		if _, exists := out["name"]; !exists {
			if fn, ok := out["full_name"].(string); ok && fn != "" {
				out["name"] = fn
			}
		}
		if _, exists := out["job_title"]; !exists {
			name, _ := out["full_name"].(string)
			for _, line := range lines[1:] {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				// Skip accessibility/noise lines common in LinkedIn cards.
				lower := strings.ToLower(line)
				if strings.HasPrefix(lower, "view ") && strings.Contains(lower, "profile") {
					continue
				}
				if strings.HasPrefix(line, "• ") || line == "Connect" || line == "Follow" ||
					line == "Message" || line == "InMail" || strings.HasPrefix(line, "Visit my") {
					continue
				}
				if strings.Contains(lower, "degree connection") || strings.Contains(lower, "mutual connection") {
					continue
				}
				if name != "" && strings.Contains(line, name) {
					continue
				}
				out["job_title"] = line
				break
			}
		}
		if _, exists := out["headline"]; !exists {
			if jt, ok := out["job_title"].(string); ok && jt != "" {
				out["headline"] = jt
			}
		}
	}

	return out
}
