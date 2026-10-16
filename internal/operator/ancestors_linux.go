//go:build linux

package operator

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/monoes/mono-agent/internal/browserctl"
	"github.com/monoes/mono-agent/internal/workflow"
)

// ancestorMarker names what marks one of this process's parents (up to
// ancestorMaxDepth, below init): MONOAGENT_WORKFLOW_RUN or
// MONOAGENT_AI_CALLER in the environment it was started with, or a
// `monomind agent exec` command line; "" when none does. A parent whose
// /proc entries can't be read (another user's) is passed over.
func ancestorMarker() string {
	if testing.Testing() && !ancestorsInTests {
		return ""
	}
	pid := os.Getppid()
	for depth := 0; depth < ancestorMaxDepth && pid > 1 && pid != ancestorsStopAt; depth++ {
		if m := processMarker(pid); m != "" {
			return m
		}
		ppid, ok := parentOf(pid)
		if !ok {
			break
		}
		pid = ppid
	}
	return ""
}

func processMarker(pid int) string {
	dir := "/proc/" + strconv.Itoa(pid)
	if env, err := os.ReadFile(dir + "/environ"); err == nil {
		for _, kv := range bytes.Split(env, []byte{0}) {
			for k, m := range map[string]string{workflow.EnvWorkflowRun: ancestorWorkflowRun, browserctl.EnvAICaller: ancestorAICaller} {
				if v, ok := bytes.CutPrefix(kv, []byte(k+"=")); ok && len(bytes.TrimSpace(v)) > 0 {
					return m
				}
			}
		}
	}
	if cmd, err := os.ReadFile(dir + "/cmdline"); err == nil && isAgentExec(bytes.Split(cmd, []byte{0})) {
		return ancestorAgentExec
	}
	return ""
}

// isAgentExec reports whether a command line runs `monomind agent exec`: an
// argument naming monomind, then agent, then exec ("node …/monomind agent
// exec …" for the script the kernel hands to node).
func isAgentExec(args [][]byte) bool {
	for i := 0; i+2 < len(args); i++ {
		if bytes.Contains([]byte(filepath.Base(string(args[i]))), []byte("monomind")) &&
			string(args[i+1]) == "agent" && string(args[i+2]) == "exec" {
			return true
		}
	}
	return false
}

// parentOf is pid's parent, from /proc/<pid>/status.
func parentOf(pid int) (int, bool) {
	status, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/status")
	if err != nil {
		return 0, false
	}
	for _, line := range bytes.Split(status, []byte{'\n'}) {
		if v, ok := bytes.CutPrefix(line, []byte("PPid:")); ok {
			ppid, err := strconv.Atoi(string(bytes.TrimSpace(v)))
			return ppid, err == nil
		}
	}
	return 0, false
}
