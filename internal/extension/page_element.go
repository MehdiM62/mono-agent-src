package extension

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
// ExtensionElement
// ---------------------------------------------------------------------------

// ExtensionElement implements browser.ElementHandle by sending commands for a
// specific element (identified by elementID) to the Chrome Extension.
type ExtensionElement struct {
	server    commandSender
	tabID     int
	elementID string
	timeout   time.Duration
}

// ElementID returns the content-script element reference ID.
func (ee *ExtensionElement) ElementID() string { return ee.elementID }

// Compile-time check.
var _ browser.ElementHandle = (*ExtensionElement)(nil)

func (ee *ExtensionElement) send(cmdType string, extra map[string]interface{}) (*Response, error) {
	params := map[string]interface{}{
		"elementId": ee.elementID,
	}
	for k, v := range extra {
		params[k] = v
	}
	return ee.server.SendCommand(&Command{
		Type:   cmdType,
		TabID:  ee.tabID,
		Params: params,
	}, ee.timeout)
}

func (ee *ExtensionElement) Click() error {
	_, err := ee.send(CmdClick, nil)
	return err
}

func (ee *ExtensionElement) Input(text string) error {
	_, err := ee.send(CmdInput, map[string]interface{}{"text": text})
	return err
}

func (ee *ExtensionElement) Text() (string, error) {
	resp, err := ee.send(CmdText, nil)
	if err != nil {
		return "", err
	}
	text, _ := resp.dataMap()["text"].(string)
	return text, nil
}

func (ee *ExtensionElement) Attribute(name string) (*string, error) {
	resp, err := ee.send(CmdAttribute, map[string]interface{}{"name": name})
	if err != nil {
		return nil, err
	}
	val, ok := resp.dataMap()["value"].(string)
	if !ok {
		return nil, nil
	}
	return &val, nil
}

func (ee *ExtensionElement) SetFiles(paths []string) error {
	// Read files on Go side and send base64 data to the extension.
	// Extensions can't read local files, so we read and encode here.
	type fileData struct {
		Name     string `json:"name"`
		Data     string `json:"data"` // base64
		MimeType string `json:"mimeType"`
	}
	var files []fileData
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return fmt.Errorf("set_files: read %s: %w", p, err)
		}
		b64 := base64.StdEncoding.EncodeToString(data)
		name := filepath.Base(p)
		mime := "application/octet-stream"
		switch {
		case strings.HasSuffix(name, ".png"):
			mime = "image/png"
		case strings.HasSuffix(name, ".jpg"), strings.HasSuffix(name, ".jpeg"):
			mime = "image/jpeg"
		case strings.HasSuffix(name, ".mp4"):
			mime = "video/mp4"
		}
		files = append(files, fileData{Name: name, Data: b64, MimeType: mime})
	}
	_, err := ee.send(CmdSetFiles, map[string]interface{}{"fileData": files})
	return err
}

func (ee *ExtensionElement) Focus() error {
	_, err := ee.send(CmdFocus, nil)
	return err
}

func (ee *ExtensionElement) ScrollIntoView() error {
	_, err := ee.send(CmdScrollIntoView, nil)
	return err
}

// WaitStable waits until the element's bounding box stops changing (two
// consecutive identical reads 100ms apart), or d elapses. content.js has no
// "stable" mode for wait_element (it requires a selector), so this polls
// get_rect instead.
func (ee *ExtensionElement) WaitStable(d time.Duration) error {
	deadline := time.Now().Add(d)
	var last map[string]interface{}
	for {
		resp, err := ee.send("get_rect", nil)
		if err != nil {
			return err
		}
		cur := resp.dataMap()
		if last != nil && rectEqual(last, cur) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("element did not settle within %s", d)
		}
		last = cur
		time.Sleep(100 * time.Millisecond)
	}
}

func rectEqual(a, b map[string]interface{}) bool {
	for _, k := range []string{"x", "y", "width", "height"} {
		if fmt.Sprint(a[k]) != fmt.Sprint(b[k]) {
			return false
		}
	}
	return true
}

func (ee *ExtensionElement) HTML() (string, error) {
	resp, err := ee.send(CmdHTML, nil)
	if err != nil {
		return "", err
	}
	html, _ := resp.dataMap()["html"].(string)
	return html, nil
}

func (ee *ExtensionElement) Property(name string) (interface{}, error) {
	resp, err := ee.send(CmdProperty, map[string]interface{}{"name": name})
	if err != nil {
		return nil, err
	}
	return resp.dataMap()["value"], nil
}
