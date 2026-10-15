package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/fsconfine"
)

// ─── Coder chat attachments and workspace images ────────────────────────────
//
// What the composer pastes, drops or picks is saved to a spool file here and
// handed to the turn as `chat --attach`; the CLI copies it into the
// conversation's folder. The chat shows images that live in that folder
// (attachments, `browser screenshot` output) through ReadWorkspaceImage,
// confined to the folder.

const (
	maxChatAttachmentBytes = 20 << 20
	maxWorkspaceImageBytes = 10 << 20
	// chatSpoolTTL is how long a saved attachment is kept: its turn copied
	// it long before.
	chatSpoolTTL = 24 * time.Hour
)

// spoolConversationID matches a conversation id usable as a folder name.
var spoolConversationID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

// unsafeSpoolRunes are what a spooled file's name loses.
var unsafeSpoolRunes = regexp.MustCompile(`[^\p{L}\p{N}._-]+`)

// SaveChatAttachment writes one attachment (base64 data, at most 20 MB) to
// ~/.monoagent/chat-spool/<conversation>/ and returns its path, for
// StartChatTurn's attachments.
func (a *App) SaveChatAttachment(conversationID, name, data string) (string, error) {
	if err := a.signedOut(); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return saveChatAttachment(filepath.Join(home, ".monoagent", "chat-spool"), conversationID, name, data, time.Now())
}

func saveChatAttachment(spool, conversationID, name, data string, now time.Time) (string, error) {
	if !spoolConversationID.MatchString(conversationID) {
		return "", fmt.Errorf("invalid conversation id %q", conversationID)
	}
	if base64.StdEncoding.DecodedLen(len(data)) > maxChatAttachmentBytes+2 {
		return "", fmt.Errorf("%s is over 20 MB", name)
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", name, err)
	}
	if len(b) > maxChatAttachmentBytes {
		return "", fmt.Errorf("%s is over 20 MB", name)
	}
	pruneChatSpool(spool, now)
	// One folder per file keeps its own name, which the agent sees.
	dir := filepath.Join(spool, conversationID, uuid.NewString())
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, spoolFileName(name))
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return "", err
	}
	return path, f.Close()
}

// spoolFileName is name as one safe file name.
func spoolFileName(name string) string {
	name = strings.TrimLeft(unsafeSpoolRunes.ReplaceAllString(filepath.Base(name), "_"), "._")
	if r := []rune(name); len(r) > 100 {
		name = string(r[len(r)-100:])
	}
	if name == "" {
		return "attachment"
	}
	return name
}

// pruneChatSpool removes saved attachments older than chatSpoolTTL, and the
// conversation folders that leaves empty. Best effort.
func pruneChatSpool(spool string, now time.Time) {
	convs, _ := os.ReadDir(spool)
	for _, c := range convs {
		if !c.IsDir() {
			continue
		}
		convDir := filepath.Join(spool, c.Name())
		saves, _ := os.ReadDir(convDir)
		for _, s := range saves {
			if fi, err := s.Info(); err == nil && now.Sub(fi.ModTime()) > chatSpoolTTL {
				_ = os.RemoveAll(filepath.Join(convDir, s.Name()))
			}
		}
		_ = os.Remove(convDir) // only when empty
	}
}

// ReadWorkspaceImage returns an image inside a coder conversation's folder
// as a data URL: a PNG, JPEG, WebP or GIF file of at most 10 MB. A path
// outside the folder (through "..", or a symlink that leads out) is refused.
// A relative path is taken from the folder.
func (a *App) ReadWorkspaceImage(conversationID, path string) (string, error) {
	if err := a.signedOut(); err != nil {
		return "", err
	}
	if a.chatSup == nil {
		return "", errors.New("chat supervisor not initialized")
	}
	cwd, err := a.chatSup.conversationCwd(a.getActiveProfileID(), conversationID)
	if err != nil {
		return "", err
	}
	return workspaceImageDataURL(cwd, path)
}

// conversationCwd is a coder conversation's folder, from `chat history
// show`; "" for any other chat. The folder is fixed when the conversation
// is created, so it is looked up once.
func (sup *chatSupervisor) conversationCwd(profileID, conversationID string) (string, error) {
	key := profileID + "\x00" + conversationID
	if cwd, ok := sup.cwds.Load(key); ok {
		return cwd.(string), nil
	}
	var rec ai.ConversationRecord
	if err := sup.cli(profileID, &rec, "chat", "history", "show", "--", conversationID); err != nil {
		return "", err
	}
	cwd := ""
	if rec.Mode == ai.ModeCoder {
		cwd = rec.Cwd
	}
	sup.cwds.Store(key, cwd)
	return cwd, nil
}

var workspaceImageExts = map[string]bool{".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true}

var workspaceImageTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/webp": true, "image/gif": true}

func workspaceImageDataURL(cwd, p string) (string, error) {
	if cwd == "" {
		return "", errors.New("this chat has no folder to show images from")
	}
	notImage := fmt.Errorf("%s is not a PNG, JPEG, WebP or GIF image", p)
	if !workspaceImageExts[strings.ToLower(filepath.Ext(p))] {
		return "", notImage
	}
	resolved, err := fsconfine.Path(fsconfine.WithRoot(context.Background(), cwd), p)
	if errors.Is(err, fsconfine.ErrOutsideWorkdir) {
		return "", fmt.Errorf("%s is outside this chat's folder %s", p, cwd)
	}
	if err != nil {
		return "", err
	}
	f, err := os.Open(resolved)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", notImage
	}
	if fi.Size() > maxWorkspaceImageBytes {
		return "", fmt.Errorf("%s is over 10 MB", p)
	}
	b, err := io.ReadAll(io.LimitReader(f, maxWorkspaceImageBytes+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxWorkspaceImageBytes {
		return "", fmt.Errorf("%s is over 10 MB", p)
	}
	mt := http.DetectContentType(b)
	if !workspaceImageTypes[mt] {
		return "", notImage
	}
	return "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(b), nil
}
