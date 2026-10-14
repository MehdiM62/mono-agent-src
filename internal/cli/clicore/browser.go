package clicore

import (
	"os/exec"
	"runtime"
)

// OpenURLInBrowser opens url in the system's default browser, cross-platform.
func OpenURLInBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		// "start" is a cmd builtin, not an executable; the empty string
		// after it is the (required, often-empty) window-title argument —
		// without it, a URL containing spaces or quotes gets misparsed as
		// the title instead of the target.
		return exec.Command("cmd", "/c", "start", "", url).Start()
	default: // linux and other freedesktop-ish systems
		return exec.Command("xdg-open", url).Start()
	}
}
