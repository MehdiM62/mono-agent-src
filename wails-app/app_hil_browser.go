package main

import (
	"os"
	"path/filepath"
)

// ReadHILScreenshot returns a browser submit approval's screenshot as a data
// URL, for its card on the Human in Loop page. `monoagentcli browser` saves
// it under ~/.monoagent/browser/hil/ (the item's readonly_data.screenshot);
// a path outside that folder, through ".." or a symlink, is refused, and so
// is anything but a PNG, JPEG, WebP or GIF image of at most 10 MB.
func (a *App) ReadHILScreenshot(path string) (string, error) {
	if err := a.signedOut(); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return imageDataURLIn(filepath.Join(home, ".monoagent", "browser", "hil"), path)
}
