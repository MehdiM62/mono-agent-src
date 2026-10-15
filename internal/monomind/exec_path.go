package monomind

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// selfBinName is the CLI's file name. A process whose executable is named
// otherwise (a test binary) adds nothing to PATH.
const selfBinName = "monoagentcli"

// selfExecutable is os.Executable; tests replace it.
var selfExecutable = os.Executable

// SelfFirstOnPath returns env with a folder that holds only a `monoagentcli`
// link to the running executable first on PATH, so `monoagentcli` in an
// agent's shell is this very build: the desktop app runs its bundled CLI,
// and the one PATH finds first may be another version. Only that one name
// is added. The executable's own folder (/usr/local/bin, say) can hold a
// node that must not shadow PinEnv's pinned one. env comes back unchanged
// when the executable isn't monoagentcli or the link can't be made.
func SelfFirstOnPath(env []string) []string {
	dir := selfBinDir()
	if dir == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	key, cur := "PATH", ""
	for _, kv := range env {
		k, v, _ := strings.Cut(kv, "=")
		if k == "PATH" || (runtime.GOOS == "windows" && strings.EqualFold(k, "PATH")) {
			key, cur = k, v
			continue
		}
		out = append(out, kv)
	}
	parts := []string{dir}
	for _, p := range filepath.SplitList(cur) {
		if p != "" && p != dir {
			parts = append(parts, p)
		}
	}
	return append(out, key+"="+strings.Join(parts, string(os.PathListSeparator)))
}

// selfBinDir is ~/.monoagent/self-bin/<hash of the executable's path>, with
// the link in it made or refreshed, or "". The folder is under the monoagent
// home, outside every sandboxed turn's writable area (exec_temp.go), so a
// turn can't swap the link a later turn runs. A symlink where the OS allows
// one, else a hard link (re-made when an update replaced the executable).
func selfBinDir() string {
	exe, err := selfExecutable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	if !strings.HasPrefix(strings.ToLower(filepath.Base(exe)), selfBinName) {
		return ""
	}
	want, err := os.Stat(exe)
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(exe))
	dir := filepath.Join(home, ".monoagent", "self-bin", hex.EncodeToString(sum[:6]))
	name := selfBinName
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	link := filepath.Join(dir, name)
	if got, err := os.Stat(link); err == nil && os.SameFile(got, want) {
		return dir
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	tmp := link + ".new-" + strconv.Itoa(os.Getpid())
	_ = os.Remove(tmp)
	if os.Symlink(exe, tmp) != nil && os.Link(exe, tmp) != nil {
		return ""
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return ""
	}
	return dir
}
