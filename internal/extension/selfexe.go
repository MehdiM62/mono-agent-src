package extension

import (
	"os"
	"strings"
)

// selfExecutable is os.Executable for a process that outlives its own
// update. The installer renames the running binary to <path>.bak and puts
// the new one at <path>, and the bridge keeps running for days: on Linux
// os.Executable then reports "<path>.bak" (and "<path>.bak (deleted)" once
// that is removed), which no longer exists. The original path holds the
// current binary, so the suffixes are stripped when that path is there.
func selfExecutable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return currentExePath(exe, fileIsRegular), nil
}

func fileIsRegular(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular()
}

// currentExePath maps a stale "<path>.bak[ (deleted)]" back to <path> when
// exists reports that <path> is there; any other path is returned as is.
func currentExePath(exe string, exists func(string) bool) string {
	orig := strings.TrimSuffix(strings.TrimSuffix(exe, " (deleted)"), ".bak")
	if orig != exe && exists(orig) {
		return orig
	}
	return exe
}
