// Package sandboxprobe guesses whether this process runs inside an agent-mode sandbox
// (`monoagentcli agent-mode sandbox`). It is advisory only: it looks for the marks such
// a sandbox leaves, and a process that wants to hide them can. Finding none proves
// nothing; never decide a security question with it.
package sandboxprobe

import (
	"errors"
	"io/fs"
	"os"
	"runtime"
	"strings"
)

// EnvMarker is set by the profiles `agent-mode sandbox` prints (bwrap sets it).
const EnvMarker = "MONOAGENT_SANDBOX"

// Inputs is what the probe looks at; Real fills it from this process.
type Inputs struct {
	Getenv    func(string) string
	StateDir  string
	ReadDir   func(string) ([]os.DirEntry, error)
	MountInfo func() (string, error) // /proc/self/mountinfo; nil or an error skips the mount check
}

// Report is the probe's answer.
type Report struct {
	Sandboxed bool     `json:"sandboxed"`
	Signals   []string `json:"signals"`
	Advisory  string   `json:"advisory"`
}

const advisory = "advisory only: a process can hide or fake these marks, and none found does not mean unsandboxed"

// Real returns the inputs for this process and the state directory stateDir.
func Real(stateDir string) Inputs {
	in := Inputs{Getenv: os.Getenv, StateDir: stateDir, ReadDir: os.ReadDir}
	if runtime.GOOS == "linux" {
		in.MountInfo = func() (string, error) {
			b, err := os.ReadFile("/proc/self/mountinfo")
			return string(b), err
		}
	}
	return in
}

// Detect looks for the marks of a sandbox.
func Detect(in Inputs) Report {
	r := Report{Signals: []string{}, Advisory: advisory}
	if v := in.Getenv(EnvMarker); v != "" {
		r.Signals = append(r.Signals, EnvMarker+"="+v+" is set")
	}
	if in.Getenv("APP_SANDBOX_CONTAINER_ID") != "" {
		r.Signals = append(r.Signals, "APP_SANDBOX_CONTAINER_ID is set (macOS App Sandbox)")
	}
	if in.StateDir != "" && in.ReadDir != nil {
		entries, err := in.ReadDir(in.StateDir)
		switch {
		case err == nil && len(entries) == 0:
			r.Signals = append(r.Signals, in.StateDir+" exists but is empty (a used state directory holds the database)")
		case errors.Is(err, fs.ErrPermission):
			r.Signals = append(r.Signals, in.StateDir+" cannot be listed (permission denied)")
		}
	}
	if in.MountInfo != nil && in.StateDir != "" {
		if txt, err := in.MountInfo(); err == nil && tmpfsAt(txt, in.StateDir) {
			r.Signals = append(r.Signals, in.StateDir+" is a tmpfs mount over the real directory")
		}
	}
	r.Sandboxed = len(r.Signals) > 0
	return r
}

// tmpfsAt reports whether mountinfo has a tmpfs mounted at dir.
func tmpfsAt(mountinfo, dir string) bool {
	for _, line := range strings.Split(mountinfo, "\n") {
		before, after, ok := strings.Cut(line, " - ")
		if !ok {
			continue
		}
		f := strings.Fields(before)
		g := strings.Fields(after)
		if len(f) > 4 && len(g) > 0 && f[4] == dir && g[0] == "tmpfs" {
			return true
		}
	}
	return false
}
