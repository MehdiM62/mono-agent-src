//go:build darwin || windows

package autostart

import (
	"path/filepath"

	"github.com/monoes/mono-agent/internal/logging"
)

// rotateDaemonLogs rotates the service's log files (launchd's
// StandardOutPath/StandardErrorPath, the scheduled task's >> redirect) before
// the service manager opens them for a start: those files are appended to
// forever otherwise. A file that cannot be renamed — still open by a
// running daemon on Windows — is appended to as before.
func rotateDaemonLogs(names ...string) {
	dir, err := logDir()
	if err != nil {
		return
	}
	for _, name := range names {
		_, _ = logging.RotateDaemonLog(filepath.Join(dir, name))
	}
}
