//go:build wfcrash && !windows

package workflow

import (
	"fmt"
	"os"
	"strings"
	"syscall"
)

// failpoint acts when the environment names this point (test builds only, tag wfcrash):
//
//	MONOAGENT_WF_FAILPOINT=<name>            the process kills itself with SIGKILL, so no deferred
//	                                         cleanup runs
//	MONOAGENT_WF_FAILPOINT_ERR=<name>        the point returns an error
//	MONOAGENT_WF_FAILPOINT_PAUSE=<name>:<p>  the process writes <p>.reached and blocks until
//	                                         something is written to the fifo <p>
func failpoint(name string) error {
	if os.Getenv("MONOAGENT_WF_FAILPOINT") == name {
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		select {}
	}
	if os.Getenv("MONOAGENT_WF_FAILPOINT_ERR") == name {
		return fmt.Errorf("failpoint %s", name)
	}
	if v := os.Getenv("MONOAGENT_WF_FAILPOINT_PAUSE"); strings.HasPrefix(v, name+":") {
		path := strings.TrimPrefix(v, name+":")
		_ = os.WriteFile(path+".reached", []byte(name), 0o600)
		if f, err := os.Open(path); err == nil {
			_, _ = f.Read(make([]byte, 1))
			f.Close()
		}
	}
	return nil
}
