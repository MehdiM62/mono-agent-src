package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"path/filepath"
	"strconv"
)

// Send approval (plan coder-bubble-v2, point 3): an agent's outbound send is a
// HIL item of kind "send" that waits for the operator. These bindings shell
// out to `monoagentcli hil …` (the CLI owns the logic, plan D11); every one
// returns the CLI's JSON as a string, or aiError's {"error","code"} shape.
// The payload is hilsend.Payload (internal/hilsend/types.go): list entries
// also appear in GetHILItems as item.kind == "send" with item.send.

// HilSendList is `hil sends [--state S]`: every send item, oldest first.
// state is "" (all) or pending|approved|rejected|sent|failed.
func (a *App) HilSendList(state string) string {
	args := []string{"hil", "sends"}
	if state != "" {
		args = append(args, "--state", state)
	}
	return a.jsonResult(args...)
}

// HilSendShow is `hil show <id>`: one item's card payload.
func (a *App) HilSendShow(id string) string { return a.jsonResult("hil", "show", id) }

// HilSendEdit is `hil edit <id>`: change the text and/or subject of a pending
// item (an empty string leaves that part alone). It stays pending; the
// returned payload is the new preview.
func (a *App) HilSendEdit(id, text, subject string) string {
	args := []string{"hil", "edit", id}
	if text != "" {
		args = append(args, "--text", text)
	}
	if subject != "" {
		args = append(args, "--subject", subject)
	}
	return a.jsonResult(args...)
}

// HilSendResolve is `hil resolve <id> --approve|--reject`. Approving performs
// the exact previewed payload once (editText / editSubject, when not empty,
// are applied first; alwaysAllow also remembers the destination) and returns
// the item in state sent or failed. It can take as long as the send does, so it
// has no short timeout.
func (a *App) HilSendResolve(id string, approve bool, editText, editSubject string, alwaysAllow bool) string {
	args := []string{"hil", "resolve", id}
	if !approve {
		args = append(args, "--reject")
	} else {
		args = append(args, "--approve")
		if editText != "" {
			args = append(args, "--edit-text", editText)
		}
		if editSubject != "" {
			args = append(args, "--edit-subject", editSubject)
		}
		if alwaysAllow {
			args = append(args, "--always-allow")
		}
	}
	var out json.RawMessage
	if err := a.runMonoCLI("", &out, args...); err != nil {
		return aiError(err)
	}
	return string(out)
}

// HilSendAttachment is `hil attachment <id> <n>`: {path,name,mime,size,kind}
// of attachment n (0-based) when it is a readable file under the profile's
// folder, the downloads folder or the coder workspace; an error otherwise.
// Pass path to OpenPathWithOS to open the file.
func (a *App) HilSendAttachment(id string, n int) string {
	return a.jsonResult("hil", "attachment", id, strconv.Itoa(n))
}

// maxSendPreviewBytes bounds the inline data URL of HilSendAttachmentData.
const maxSendPreviewBytes = 10 * 1024 * 1024

// HilSendAttachmentData returns attachment n as a base64 data URL for inline
// preview (images in the card). The CLI decides which file may be read; this
// only encodes the path it returned.
func (a *App) HilSendAttachmentData(id string, n int) (string, error) {
	var f struct {
		Path string `json:"path"`
		Mime string `json:"mime"`
		Size int64  `json:"size"`
	}
	if err := a.runMonoCLI("", &f, "hil", "attachment", id, strconv.Itoa(n)); err != nil {
		return "", err
	}
	if f.Size > maxSendPreviewBytes {
		return "", fmt.Errorf("attachment too large to preview inline (%d MB) — open it instead", f.Size/1024/1024)
	}
	data, err := os.ReadFile(f.Path)
	if err != nil {
		return "", fmt.Errorf("reading attachment: %w", err)
	}
	mt := f.Mime
	if mt == "" {
		mt = mime.TypeByExtension(filepath.Ext(f.Path))
	}
	if mt == "" {
		mt = "application/octet-stream"
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// HilSendRecipient is `hil recipient <id>`: the recipient's people record
// ({found, personId, name, profileUrl, platform, username, fullName, imageUrl,
// contactDetails, website, introduction, jobTitle, category}).
func (a *App) HilSendRecipient(id string) string { return a.jsonResult("hil", "recipient", id) }

// HilSendAllowList is `hil allow list`: the always-allow rules.
func (a *App) HilSendAllowList() string { return a.jsonResult("hil", "allow", "list") }

// HilSendAllowAdd is `hil allow add`: always allow one destination.
func (a *App) HilSendAllowAdd(action, platform, account, recipient string) string {
	return a.jsonResult("hil", "allow", "add", "--action", action, "--platform", platform, "--account", account, "--recipient", recipient)
}

// HilSendAllowRemove is `hil allow remove <rule-id>`.
func (a *App) HilSendAllowRemove(ruleID string) string {
	return a.jsonResult("hil", "allow", "remove", ruleID)
}

// HilSendSetApproval is `coder set --send-approval=<on>` (default on): whether
// an agent's sends wait for approval. Returns coder status.
func (a *App) HilSendSetApproval(on bool) string {
	return a.jsonResult("coder", "set", "--send-approval="+strconv.FormatBool(on))
}
