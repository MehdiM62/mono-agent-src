package extension

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"regexp"

	"github.com/monoes/mono-agent/internal/ai/chatevents"
	"github.com/monoes/mono-agent/internal/fsconfine"
)

// coder.events shows the side panel the images a coder turn's tools named
// (a `browser screenshot --json` {path}, a file the agent wrote): a PNG,
// JPEG or WebP path in a tool result that resolves inside the conversation's
// folder, symlinks and `..` included (internal/fsconfine), at most
// coderImageMax each. Anything else in the folder or outside it is never
// read.

const (
	coderImageMax       = 2 << 20
	coderImagesPerReply = 6
)

// coderImagePath finds image paths in tool output: a run of characters that
// can't end a quoted or listed path, ending in an image extension.
var coderImagePath = regexp.MustCompile(`[^\s"'<>|*?(){}\[\],;]+\.(?i:png|jpe?g|webp)\b`)

type coderImage struct {
	Seq     int64  `json:"seq"`
	Path    string `json:"path"`
	DataURL string `json:"dataUrl"`
}

// coderImages reads the images events' tool results name inside cwd.
func coderImages(cwd string, events []ChatEvent) []coderImage {
	out := []coderImage{}
	if cwd == "" {
		return out
	}
	confined := fsconfine.WithRoot(context.Background(), cwd)
	seen := map[string]bool{}
	for _, ev := range events {
		if ev.Type != string(chatevents.EventToolCompleted) {
			continue
		}
		var p chatevents.ToolCompletedPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			continue
		}
		for _, named := range coderImagePath.FindAllString(p.Result, -1) {
			if len(out) == coderImagesPerReply {
				return out
			}
			resolved, err := fsconfine.Path(confined, named)
			if err != nil || seen[resolved] {
				continue
			}
			seen[resolved] = true
			if url, ok := imageDataURL(resolved); ok {
				out = append(out, coderImage{Seq: ev.Seq, Path: named, DataURL: url})
			}
		}
	}
	return out
}

// imageDataURL reads p when it is a regular PNG, JPEG or WebP file of at
// most coderImageMax bytes.
func imageDataURL(p string) (string, bool) {
	b, ok := readImageFile(p, coderImageMax)
	if !ok {
		return "", false
	}
	switch mime := http.DetectContentType(b); mime {
	case "image/png", "image/jpeg", "image/webp":
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b), true
	}
	return "", false
}

// readImageFile reads the regular file p of at most max bytes. It looks at
// the path first, so a pipe or device is never opened, then opens it once,
// checks the open file is the one it looked at, and reads at most max+1
// bytes: a file swapped or grown in between can't slip past the cap.
func readImageFile(p string, max int64) ([]byte, bool) {
	before, err := os.Stat(p)
	if err != nil || !before.Mode().IsRegular() || before.Size() > max {
		return nil, false
	}
	f, err := os.Open(p)
	if err != nil {
		return nil, false
	}
	defer f.Close()
	if now, err := f.Stat(); err != nil || !os.SameFile(before, now) {
		return nil, false
	}
	return readAtMost(f, max)
}

// readAtMost reads r to its end, refusing more than max bytes.
func readAtMost(r io.Reader, max int64) ([]byte, bool) {
	b, err := io.ReadAll(io.LimitReader(r, max+1))
	if err != nil || int64(len(b)) > max {
		return nil, false
	}
	return b, true
}
