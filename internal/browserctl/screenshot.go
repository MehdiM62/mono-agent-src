package browserctl

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// screenshotTimeout bounds one capture; a full page can take a while.
const screenshotTimeout = 60 * time.Second

// ScreenshotOptions are the screenshot command's parameters.
type ScreenshotOptions struct {
	// Mode is "viewport" (default), "full", "element" or "clip".
	Mode string
	// Selector is the element for Mode "element".
	Selector string
	// Clip is the page area for Mode "clip".
	Clip *Clip
	// Format is "png" (default) or "jpeg"; Quality applies to jpeg.
	Format  string
	Quality int
}

// Clip is a page area in CSS pixels.
type Clip struct {
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Shot is a captured image.
type Shot struct {
	Data   []byte
	Mime   string
	Width  int
	Height int
}

// Ext is the file extension for the shot's type, with the dot.
func (s *Shot) Ext() string {
	if s.Mime == "image/jpeg" {
		return ".jpg"
	}
	return ".png"
}

// Screenshot captures the tab.
func (s *Session) Screenshot(o ScreenshotOptions) (*Shot, error) {
	mode := o.Mode
	if mode == "" {
		mode = "viewport"
	}
	params := map[string]interface{}{"mode": mode}
	if o.Selector != "" {
		params["selector"] = o.Selector
	}
	if o.Clip != nil {
		params["clip"] = o.Clip
	}
	if o.Format != "" {
		params["format"] = o.Format
	}
	if o.Quality > 0 {
		params["quality"] = o.Quality
	}
	var out struct {
		Data   string `json:"data"`
		Mime   string `json:"mime"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	if err := s.Call(CmdScreenshot, params, screenshotTimeout, &out); err != nil {
		return nil, err
	}
	data, err := base64.StdEncoding.DecodeString(out.Data)
	if err != nil || len(data) == 0 {
		return nil, errors.New("screenshot: the extension sent no image")
	}
	if out.Mime == "" {
		out.Mime = "image/png"
	}
	return &Shot{Data: data, Mime: out.Mime, Width: out.Width, Height: out.Height}, nil
}

// Emulation is the emulate command's parameters; zero fields are left alone.
type Emulation struct {
	Width             int
	Height            int
	DeviceScaleFactor float64
	Mobile            bool
	// ColorScheme is "dark" or "light"; ReducedMotion "reduce" or
	// "no-preference".
	ColorScheme   string
	ReducedMotion string
	// Reset drops every override.
	Reset bool
}

// Emulate sets (or with e.Reset clears) the tab's viewport and media
// overrides.
func (s *Session) Emulate(e Emulation) error {
	params := map[string]interface{}{}
	if e.Reset {
		params["reset"] = true
	}
	if e.Width > 0 {
		params["width"] = e.Width
	}
	if e.Height > 0 {
		params["height"] = e.Height
	}
	if e.DeviceScaleFactor > 0 {
		params["deviceScaleFactor"] = e.DeviceScaleFactor
	}
	if e.Mobile {
		params["mobile"] = true
	}
	media := map[string]interface{}{}
	if e.ColorScheme != "" {
		media["prefersColorScheme"] = e.ColorScheme
	}
	if e.ReducedMotion != "" {
		media["prefersReducedMotion"] = e.ReducedMotion
	}
	if len(media) > 0 {
		params["media"] = media
	}
	_, err := s.Send(CmdEmulate, params, DefaultTimeout)
	return err
}

// ScreenshotsDir is where screenshots go when no path is given:
// <dir>/.monoagent/screenshots.
func ScreenshotsDir(dir string) string { return filepath.Join(dir, ".monoagent", "screenshots") }

// WriteScreenshot writes shot to out, or when out is "" to
// ScreenshotsDir(dir)/<yyyymmdd-hhmmss>-<n><ext>, n making the name new.
// It returns the absolute path written. dir "" is the working directory.
func WriteScreenshot(dir, out string, shot *Shot) (string, error) {
	if shot == nil || len(shot.Data) == 0 {
		return "", errors.New("no image to write")
	}
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", err
		}
		dir = wd
	}
	if out != "" {
		path := out
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, shot.Data, 0o644); err != nil {
			return "", err
		}
		return filepath.Abs(path)
	}
	shots := ScreenshotsDir(dir)
	if err := os.MkdirAll(shots, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	for n := 1; n < 10000; n++ {
		path := filepath.Join(shots, fmt.Sprintf("%s-%d%s", stamp, n, shot.Ext()))
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(shot.Data); err != nil {
			f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return filepath.Abs(path)
	}
	return "", fmt.Errorf("no free screenshot name in %s", shots)
}

// MediaFlag checks a --media value ("dark" or "light").
func MediaFlag(v string) (string, error) {
	switch v = strings.ToLower(strings.TrimSpace(v)); v {
	case "", "dark", "light":
		return v, nil
	}
	return "", fmt.Errorf("--media is dark or light, not %q", v)
}
