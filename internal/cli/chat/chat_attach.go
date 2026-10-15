package chat

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/ai/chatevents"
	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/monomind"
)

// Attachments on coder turns (`chat --attach PATH`): each file is copied
// into the folder the agent works in, under .monoagent/attachments/<turn>/,
// and the prompt lists the copies, which the agent opens with its own file
// tools. A monomind with agent-exec-attachments also gets the copies as
// `agent exec --attach`, which hands images to the model directly.

const (
	maxAttachments     = 10
	maxAttachmentBytes = 20 << 20
)

// attachmentSources checks the --attach files before anything runs and
// returns their absolute paths: at most maxAttachments regular files (a
// folder is refused) of at most maxAttachmentBytes each.
func attachmentSources(paths []string) ([]string, error) {
	if len(paths) > maxAttachments {
		return nil, clicore.InvalidInput("--attach takes at most %d files, got %d", maxAttachments, len(paths))
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		abs, err := filepath.Abs(expandHome(p))
		if err != nil {
			return nil, clicore.InvalidInput("--attach %s: %v", p, err)
		}
		fi, err := os.Stat(abs)
		switch {
		case err != nil:
			return nil, clicore.InvalidInput("--attach: %v", err)
		case fi.IsDir():
			return nil, clicore.InvalidInput("--attach %s is a folder; attach the files in it", p)
		case !fi.Mode().IsRegular():
			return nil, clicore.InvalidInput("--attach %s is not a regular file", p)
		case fi.Size() > maxAttachmentBytes:
			return nil, clicore.InvalidInput("--attach %s is %s; the limit is %s per file", p, formatSize(fi.Size()), formatSize(maxAttachmentBytes))
		}
		out = append(out, abs)
	}
	return out, nil
}

// attachTurnDir matches a turn id that can name its attachments folder.
var attachTurnDir = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// journaledAttachments checks a journaled turn's --attach files before the
// turn is admitted: only a coder conversation takes them, and the turn id
// names their folder.
func journaledAttachments(store *ai.AIStore, profileID, conversationID, turnID string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	// An unknown conversation is left to the admission, which says so.
	if conv, err := store.GetConversation(conversationID, profileID); err == nil && conv.Mode != ai.ModeCoder {
		return nil, clicore.InvalidInput("--attach applies only to coder chats; conversation %s is an assistant chat", conversationID)
	}
	if !attachTurnDir.MatchString(turnID) {
		return nil, clicore.InvalidInput("--attach needs a --turn id of letters, digits, '.', '_' and '-' (it names the attachments folder)")
	}
	return attachmentSources(paths)
}

// copyAttachments copies srcs (checked by attachmentSources) into
// cwd/.monoagent/attachments/<dir>/ under safe, distinct names and
// describes the copies. A copy never replaces a file that is already there.
func copyAttachments(cwd, dir string, srcs []string) ([]chatevents.TurnAttachment, error) {
	if len(srcs) == 0 {
		return nil, nil
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return nil, clicore.InvalidInput("this coder conversation's folder %s no longer exists; start a new conversation", cwd)
	}
	base := filepath.Join(cwd, ".monoagent")
	target := filepath.Join(base, "attachments", dir)
	if err := os.MkdirAll(target, 0o700); err != nil {
		return nil, fmt.Errorf("creating the attachments folder: %w", err)
	}
	ignoreAll(base)
	used := map[string]bool{}
	atts := make([]chatevents.TurnAttachment, 0, len(srcs))
	for _, src := range srcs {
		name := uniqueName(safeFileName(filepath.Base(src)), used)
		dst := filepath.Join(target, name)
		size, head, err := copyAttachment(src, dst)
		if err != nil {
			return nil, fmt.Errorf("attaching %s: %w", src, err)
		}
		atts = append(atts, chatevents.TurnAttachment{Name: name, Path: dst, Mime: attachmentMime(name, head), Size: size})
	}
	return atts, nil
}

// copyAttachment copies src to the new file dst and returns its size and
// first bytes (for the media type). A file that grew past the limit since
// it was checked is refused.
func copyAttachment(src, dst string) (int64, []byte, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, nil, err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return 0, nil, err
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(in, head)
	head = head[:n]
	_, werr := out.Write(head)
	m, cerr := io.Copy(out, io.LimitReader(in, maxAttachmentBytes+1-int64(n)))
	size := int64(n) + m
	if err := errors.Join(werr, cerr, out.Close()); err != nil || size > maxAttachmentBytes {
		os.Remove(dst)
		if err == nil {
			err = fmt.Errorf("it is over %s", formatSize(maxAttachmentBytes))
		}
		return 0, nil, err
	}
	return size, head, nil
}

// ignoreAll keeps git out of dir (the folder's .monoagent): attachments are
// the user's files for this chat, not part of the project.
func ignoreAll(dir string) {
	if f, err := os.OpenFile(filepath.Join(dir, ".gitignore"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644); err == nil {
		_, _ = f.WriteString("*\n")
		f.Close()
	}
}

// unsafeNameRunes are what a copied file's name loses: anything but
// letters, digits, '.', '_' and '-'.
var unsafeNameRunes = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

// safeFileName is name made safe as a single file name in any shell: no
// separators, spaces or leading dots, at most 100 characters.
func safeFileName(name string) string {
	name = strings.TrimLeft(unsafeNameRunes.ReplaceAllString(name, "_"), "._")
	if r := []rune(name); len(r) > 100 {
		ext := []rune(filepath.Ext(name))
		if len(ext) > 10 {
			ext = nil
		}
		name = string(r[:100-len(ext)]) + string(ext)
	}
	if name == "" {
		return "file"
	}
	return name
}

// uniqueName returns name, or name-2, name-3, … when used already has it.
func uniqueName(name string, used map[string]bool) string {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	cand := name
	for i := 2; used[strings.ToLower(cand)]; i++ {
		cand = stem + "-" + strconv.Itoa(i) + ext
	}
	used[strings.ToLower(cand)] = true
	return cand
}

// attachmentMime is a file's media type, from its extension, else its
// first bytes.
func attachmentMime(name string, head []byte) string {
	t := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if t == "" {
		t = http.DetectContentType(head)
	}
	if mt, _, err := mime.ParseMediaType(t); err == nil {
		return mt
	}
	return t
}

// attachmentPrompt is the block a turn's prompt ends with when it has
// attachments, "" when it has none.
func attachmentPrompt(atts []chatevents.TurnAttachment) string {
	if len(atts) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n\nAttached files (the user attached these to this message; the copies are in your working folder):\n")
	for _, a := range atts {
		fmt.Fprintf(&b, "- %s (%s, %s)\n", a.Path, a.Mime, formatSize(a.Size))
	}
	b.WriteString("Open them with your file-reading tool; it shows you images as well as text.")
	return b.String()
}

// nativeAttachments are the copies to pass as `agent exec --attach`: all of
// them when monomind takes the flag, none otherwise (the prompt still lists
// them).
func nativeAttachments(caps *monomind.CapabilitySet, atts []chatevents.TurnAttachment) []string {
	if !caps.Has(monomind.CapAgentExecAttachments) {
		return nil
	}
	paths := make([]string, 0, len(atts))
	for _, a := range atts {
		paths = append(paths, a.Path)
	}
	return paths
}

// formatSize renders n bytes as B, KB or MB.
func formatSize(n int64) string {
	switch {
	case n < 1<<10:
		return strconv.FormatInt(n, 10) + " B"
	case n < 1<<20:
		return strconv.FormatInt((n+1<<9)>>10, 10) + " KB"
	default:
		return strconv.FormatFloat(float64(n)/(1<<20), 'f', 1, 64) + " MB"
	}
}
