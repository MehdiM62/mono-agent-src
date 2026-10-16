//go:build !linux && !darwin && !windows

package browserctl

// processStart can't tell when a process started on this system, so no
// kind token counts here (the shell runs as kind other).
func processStart(int) (string, bool) { return "", false }
