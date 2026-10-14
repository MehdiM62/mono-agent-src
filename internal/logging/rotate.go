package logging

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// Daemon log rotation: daemon.log is rotated when it is opened (a daemon
// start) and has grown past DaemonLogMaxBytes, keeping DaemonLogKeep old
// files (daemon.log.1 newest … daemon.log.3 oldest).
const (
	DaemonLogMaxBytes = 10 << 20
	DaemonLogKeep     = 3
)

// Rotate renames path to path.1 when it is larger than maxBytes, shifting
// path.1 … path.(keep-1) up by one and dropping path.keep. A missing or
// small file is left alone. It reports whether it rotated.
func Rotate(path string, maxBytes int64, keep int) (bool, error) {
	fi, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if fi.Size() <= maxBytes || keep < 1 {
		return false, nil
	}
	if err := os.Remove(fmt.Sprintf("%s.%d", path, keep)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	for i := keep - 1; i >= 1; i-- {
		from := fmt.Sprintf("%s.%d", path, i)
		if err := os.Rename(from, fmt.Sprintf("%s.%d", path, i+1)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	if err := os.Rename(path, path+".1"); err != nil {
		return false, err
	}
	return true, nil
}

// RotateDaemonLog is Rotate with the daemon log's limits.
func RotateDaemonLog(path string) (bool, error) {
	return Rotate(path, DaemonLogMaxBytes, DaemonLogKeep)
}
