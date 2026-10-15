package extension

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	fi, err := os.Stat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > coderImageMax {
		return "", false
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", false
	}
	switch mime := http.DetectContentType(b); mime {
	case "image/png", "image/jpeg", "image/webp":
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(b), true
	}
	return "", false
}
