// wails-app/app_documents.go
package main

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// ProfileDocument mirrors internal/vault.DocumentEntry's fields (that
// struct has no json tags -> PascalCase on the wire), remapped to a
// stable lowercase contract for the frontend.
type ProfileDocument struct {
	ID            string `json:"id"`
	Filename      string `json:"filename"`
	Path          string `json:"path"`
	SizeBytes     int64  `json:"size_bytes"`
	Source        string `json:"source"`
	ApplicationID string `json:"application_id"`
	CreatedAt     string `json:"created_at"`
	Indexed       bool   `json:"indexed"`
	IndexError    string `json:"index_error"`
	Stale         bool   `json:"stale"`
	// URL and CaptureDir are set only for a browser capture: the page it
	// was saved from, and its envelope directory in the profile's inbox.
	URL        string `json:"url"`
	CaptureDir string `json:"capture_dir"`
	// SummaryStatus is a capture's AI summary state (pending, running,
	// done, error, stalled), "" when none was asked for.
	SummaryStatus string `json:"summary_status"`
}

// KnowledgeSearchResult mirrors internal/monomind.KnowledgeResult (also
// tagless -> PascalCase on the wire).
type KnowledgeSearchResult struct {
	Path    string  `json:"path"`
	Excerpt string  `json:"excerpt"`
	Score   float64 `json:"score"`
}

// ListProfileDocuments lists the active profile's documents: uploads,
// files discovered in its folder, and browser captures (the CLI syncs the
// profile's capture inbox into the list before returning it).
func (a *App) ListProfileDocuments() ([]ProfileDocument, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	var raw []struct {
		ID, Path, Filename, Source, ApplicationID, CreatedAt, IndexError, URL, CaptureDir, SummaryStatus string
		SizeBytes                                                                                        int64
		Indexed                                                                                          bool
		Stale                                                                                            bool
	}
	if err := a.runMonoCLI("", &raw, "profile", "documents", "list"); err != nil {
		return nil, err
	}
	out := make([]ProfileDocument, 0, len(raw))
	for _, d := range raw {
		out = append(out, ProfileDocument{
			ID: d.ID, Filename: d.Filename, Path: d.Path, SizeBytes: d.SizeBytes,
			Source: d.Source, ApplicationID: d.ApplicationID, CreatedAt: d.CreatedAt,
			Indexed: d.Indexed, IndexError: d.IndexError, Stale: d.Stale,
			URL: d.URL, CaptureDir: d.CaptureDir, SummaryStatus: d.SummaryStatus,
		})
	}
	return out, nil
}

// GetProfileDocument returns one document by id, scoped to the active
// profile (`profile documents get`) — a single-row counterpart to
// ListProfileDocuments for callers (chatArtifacts.js's resolveArtifact)
// that only need to re-validate one id rather than pay for and scan the
// whole vault. nil+error on not-found rather than an empty result.
func (a *App) GetProfileDocument(id string) (*ProfileDocument, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	var doc ProfileDocument
	if err := a.cliJSON(profileCLITimeout, &doc, "profile", "documents", "get", id); err != nil {
		return nil, err
	}
	return &doc, nil
}

// UploadResult is UploadProfileDocument's return value.
type UploadResult struct {
	ID         string `json:"id"`
	Indexed    bool   `json:"indexed"`
	IndexError string `json:"index_error"`
}

// UploadProfileDocument registers path in the vault and indexes it for
// knowledge search. source defaults to "upload" (the CLI's own default)
// if empty. The upload itself succeeds even if indexing fails (e.g.
// monomind isn't installed or this profile isn't monomind-init'd) --
// Indexed/IndexError report that outcome so the caller can show it.
func (a *App) UploadProfileDocument(path, source string) (*UploadResult, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	args := []string{"profile", "upload-document", path}
	if source != "" {
		args = append(args, "--source", source)
	}
	var result UploadResult
	if err := a.runMonoCLI("", &result, args...); err != nil {
		return nil, err
	}
	return &result, nil
}

// DeleteProfileDocument removes a document from the vault.
func (a *App) DeleteProfileDocument(id string) error {
	if err := a.signedOut(); err != nil {
		return err
	}
	return a.runMonoCLI("", nil, "profile", "documents", "rm", id)
}

// IndexProfileDocument runs (or re-runs) knowledge_ingest for a single
// already-tracked document -- the on-demand action for a "Not indexed" or
// "Stale" row; never triggered automatically by discovery. Reuses
// UploadResult's shape exactly so the frontend can treat a manual index
// and an upload's own immediate index attempt identically.
func (a *App) IndexProfileDocument(id string) (*UploadResult, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	var result UploadResult
	if err := a.runMonoCLI("", &result, "profile", "documents", "index", id); err != nil {
		return nil, err
	}
	return &result, nil
}

// SearchProfileKnowledge searches indexed profile documents (the same
// search chat uses automatically).
func (a *App) SearchProfileKnowledge(query string) ([]KnowledgeSearchResult, error) {
	if err := a.signedOut(); err != nil {
		return nil, err
	}
	var raw []struct {
		Path, Excerpt string
		Score         float64
	}
	if err := a.runMonoCLI("", &raw, "profile", "search-knowledge", query); err != nil {
		return nil, err
	}
	out := make([]KnowledgeSearchResult, 0, len(raw))
	for _, r := range raw {
		out = append(out, KnowledgeSearchResult{Path: r.Path, Excerpt: r.Excerpt, Score: r.Score})
	}
	return out, nil
}

// OpenAnyFilePicker opens a native file picker with no extension filter
// (profile documents can be PDF, DOCX, plain text, etc.) and returns the
// selected path (empty string if cancelled).
func (a *App) OpenAnyFilePicker(title string) string {
	if err := a.signedOut(); err != nil {
		return loginRequiredJSON(err)
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil {
		return ""
	}
	return path
}

// OpenAnyFilesPicker is OpenAnyFilePicker with multi-select: it returns every
// chosen path (empty if cancelled).
func (a *App) OpenAnyFilesPicker(title string) []string {
	if a.signedOut() != nil {
		return nil
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil {
		return nil
	}
	return paths
}

// Caps for a folder upload: choosing a whole home folder must not queue an
// unbounded import.
const (
	maxFolderFiles = 5000
	maxFolderBytes = 2 << 30
)

// walkFolderFiles lists the regular files under dir, recursively. Hidden
// entries (dot-names below dir; dir itself may be dot-named), symlinks and
// unreadable entries are skipped and reported in skipped. over is set, and the
// walk stops, once the file count or total size passes the caps.
func walkFolderFiles(dir string) (files, skipped []string, total int64, over bool) {
	stop := errors.New("over cap")
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			skipped = append(skipped, p)
			return nil
		}
		if p != dir && strings.HasPrefix(d.Name(), ".") {
			skipped = append(skipped, p)
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if !d.Type().IsRegular() || ierr != nil {
			skipped = append(skipped, p)
			return nil
		}
		files = append(files, p)
		total += info.Size()
		if len(files) > maxFolderFiles || total > maxFolderBytes {
			over = true
			return stop
		}
		return nil
	})
	return files, skipped, total, over
}

// OpenFolderFilesPicker opens a folder picker and returns the files to
// upload from the chosen folder, after the user confirms the file count and
// total size. Empty if cancelled, declined, or over the caps.
func (a *App) OpenFolderFilesPicker(title string) []string {
	if a.signedOut() != nil {
		return nil
	}
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil || dir == "" {
		return nil
	}
	files, skipped, total, over := walkFolderFiles(dir)
	if over {
		_, _ = runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
			Type: runtime.ErrorDialog, Title: "Folder too large",
			Message: fmt.Sprintf("%s holds more than %d files or %d GB. Choose a smaller folder.", dir, maxFolderFiles, maxFolderBytes>>30),
		})
		return nil
	}
	if len(files) == 0 {
		return nil
	}
	msg := fmt.Sprintf("Upload %d files (%.1f MB) from %s?", len(files), float64(total)/(1<<20), dir)
	if len(skipped) > 0 {
		shown := skipped
		if len(shown) > 10 {
			shown = shown[:10]
		}
		msg += fmt.Sprintf("\n\n%d hidden, symlinked or unreadable entries will be skipped:\n%s", len(skipped), strings.Join(shown, "\n"))
	}
	choice, err := runtime.MessageDialog(a.ctx, runtime.MessageDialogOptions{
		Type: runtime.QuestionDialog, Title: "Upload folder", Message: msg,
		Buttons: []string{"Upload", "Cancel"}, DefaultButton: "Cancel", CancelButton: "Cancel",
	})
	if err != nil || choice != "Upload" {
		return nil
	}
	return files
}
