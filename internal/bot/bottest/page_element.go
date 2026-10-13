package bottest

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/browser"
)

// ---------------------------------------------------------------------------
// Element
// ---------------------------------------------------------------------------

// Element implements browser.ElementHandle like *extension.ExtensionElement:
// a registry id that goes stale when the document is replaced.
type Element struct {
	p  *Page
	id string
}

var _ browser.ElementHandle = (*Element)(nil)

// ElementID returns the registry id (as ExtensionElement.ElementID does).
func (e *Element) ElementID() string { return e.id }

func (e *Element) call(fn string, extra map[string]interface{}) (interface{}, error) {
	params := map[string]interface{}{"id": e.id}
	for k, v := range extra {
		params[k] = v
	}
	return e.p.rt(fn, params)
}

// Click: scrollIntoView, synthetic mousedown/mouseup, el.click() — untrusted.
func (e *Element) Click() error { _, err := e.call("click", nil); return err }

// Input focuses, clears, then types text per character (contenteditable:
// paste → execCommand → textContent).
func (e *Element) Input(text string) error {
	_, err := e.call("input", map[string]interface{}{"text": text})
	return err
}

// Text returns the trimmed textContent.
func (e *Element) Text() (string, error) {
	v, err := e.call("text", nil)
	s, _ := v.(string)
	return s, err
}

// Attribute returns nil when the attribute is missing.
func (e *Element) Attribute(name string) (*string, error) {
	v, err := e.call("attr", map[string]interface{}{"name": name})
	if err != nil {
		return nil, err
	}
	s, ok := v.(string)
	if !ok {
		return nil, nil
	}
	return &s, nil
}

// Property returns el[name] as JSON-decoded data.
func (e *Element) Property(name string) (interface{}, error) {
	return e.call("prop", map[string]interface{}{"name": name})
}

// HTML returns innerHTML (ExtensionElement's default).
func (e *Element) HTML() (string, error) {
	v, err := e.call("html", nil)
	s, _ := v.(string)
	return s, err
}

// Focus calls el.focus() and dispatches a synthetic bubbling focus event.
func (e *Element) Focus() error { _, err := e.call("focus", nil); return err }

// ScrollIntoView centres the element (smooth).
func (e *Element) ScrollIntoView() error { _, err := e.call("scrollIntoView", nil); return err }

// WaitStable waits until the element's box is unchanged across d.
func (e *Element) WaitStable(d time.Duration) error {
	if d <= 0 {
		d = 100 * time.Millisecond
	}
	_, err := e.call("stable", map[string]interface{}{"ms": d.Milliseconds()})
	return err
}

// SetFiles reads the files in Go and assigns them to a file input through a
// DataTransfer, firing change and input — the extension's approach.
func (e *Element) SetFiles(paths []string) error {
	type fileData struct {
		Name     string `json:"name"`
		Data     string `json:"data"`
		MimeType string `json:"mimeType"`
	}
	var files []fileData
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("set_files: read %s: %w", p, err)
		}
		name := filepath.Base(p)
		mt := "application/octet-stream"
		switch {
		case strings.HasSuffix(name, ".png"):
			mt = "image/png"
		case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
			mt = "image/jpeg"
		case strings.HasSuffix(name, ".mp4"):
			mt = "video/mp4"
		}
		files = append(files, fileData{Name: name, Data: base64.StdEncoding.EncodeToString(data), MimeType: mt})
	}
	_, err := e.call("setFiles", map[string]interface{}{"fileData": files})
	return err
}
