package chat

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monoes/mono-agent/internal/cli/clicore"
	"github.com/monoes/mono-agent/internal/coderhost"
	"github.com/monoes/mono-agent/internal/daemonhb"
)

// execLauncher starts a session's turn as the same `chat --conversation`
// process the desktop app starts, but detached (its own session, output in a
// log file), so it survives the host and the app. The turn journals itself.
type execLauncher struct {
	bin, dbPath, instance, logDir string
	logf                          func(string, ...any)
}

// hostID is what makes a host-owned name safe to put in a file name and a flag.
var hostID = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9._-]{0,127}$`)

// hostTurnGrace is how long a stopped turn gets to end before it is killed.
var hostTurnGrace = 15 * time.Second

func (l *execLauncher) args(profileID string, rest ...string) []string {
	return append([]string{"--db-path", l.dbPath, "--profile", profileID, "--json"}, rest...)
}

func (l *execLauncher) Start(profileID, conversationID, turnID, message string, attachments []string) (coderhost.Turn, error) {
	if !hostID.MatchString(conversationID) || !hostID.MatchString(turnID) {
		return nil, fmt.Errorf("conversation and turn ids must be letters, digits, '.', '_' and '-'")
	}
	if err := os.MkdirAll(l.logDir, 0o700); err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(l.logDir, turnID+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	rest := []string{"chat", "--conversation", conversationID, "--turn", turnID, "--instance", l.instance}
	for _, a := range attachments {
		rest = append(rest, "--attach="+a)
	}
	cmd := exec.Command(l.bin, l.args(profileID, append(rest, "--", message)...)...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	if err := clicore.DetachStart(cmd); err != nil {
		return nil, err
	}
	t := &execTurn{pid: cmd.Process.Pid, stamp: pidStamp(cmd.Process.Pid), done: make(chan struct{})}
	go func() {
		t.err = cmd.Wait()
		close(t.done)
	}()
	return t, nil
}

func (l *execLauncher) Adopt(pid int, stamp string) coderhost.Turn {
	if pid <= 0 || !daemonhb.ProcessAlive(pid) {
		return nil
	}
	if stamp != "" && pidStamp(pid) != stamp {
		return nil
	}
	return &adoptedTurn{pid: pid, stamp: stamp}
}

// Finish records a turn whose process ended without finishing it, through
// `chat history finish`, which does nothing for a turn that finished itself.
func (l *execLauncher) Finish(profileID, conversationID, turnID, reason string) {
	if turnID == "" || !hostID.MatchString(conversationID) || !hostID.MatchString(turnID) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, l.bin, l.args(profileID, "chat", "history", "finish", conversationID, turnID, "--status", "failed", "--reason", reason)...).CombinedOutput()
	if err != nil && l.logf != nil {
		l.logf("coder host: closing turn %s: %v: %s", turnID, err, strings.TrimSpace(string(out)))
	}
}

type execTurn struct {
	pid   int
	stamp string
	done  chan struct{}
	err   error
	once  sync.Once
}

func (t *execTurn) PID() int      { return t.pid }
func (t *execTurn) Stamp() string { return t.stamp }
func (t *execTurn) Wait() error   { <-t.done; return t.err }
func (t *execTurn) Stop()         { t.once.Do(func() { stopGroup(t.pid, t.done) }) }

// adoptedTurn is a turn process a previous host started: not this process's
// child, so it is watched by pid.
type adoptedTurn struct {
	pid   int
	stamp string
	once  sync.Once
}

func (t *adoptedTurn) PID() int      { return t.pid }
func (t *adoptedTurn) Stamp() string { return t.stamp }

func (t *adoptedTurn) gone() bool {
	return !daemonhb.ProcessAlive(t.pid) || (t.stamp != "" && pidStamp(t.pid) != t.stamp)
}

func (t *adoptedTurn) Wait() error {
	for !t.gone() {
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func (t *adoptedTurn) Stop() {
	t.once.Do(func() {
		done := make(chan struct{})
		go func() { _ = t.Wait(); close(done) }()
		stopGroup(t.pid, done)
	})
}

// stopGroup asks the turn's process group to end, and kills it when it has not
// within hostTurnGrace. It returns at once.
func stopGroup(pid int, done <-chan struct{}) {
	_ = terminateGroup(pid)
	time.AfterFunc(hostTurnGrace, func() {
		select {
		case <-done:
		default:
			_ = killGroup(pid)
		}
	})
}

// pidStamp says what makes pid the process it is now: its start time. "" when
// it cannot be read (the process is gone, or Windows).
func pidStamp(pid int) string {
	switch runtime.GOOS {
	case "windows":
		return ""
	case "linux":
		raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
		if err != nil {
			return ""
		}
		s := string(raw)
		i := strings.LastIndex(s, ")")
		if i < 0 {
			return ""
		}
		f := strings.Fields(s[i+1:])
		if len(f) < 20 {
			return ""
		}
		return "start:" + f[19]
	}
	out, err := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if s := strings.TrimSpace(string(out)); err == nil && s != "" {
		return "started:" + s
	}
	return ""
}
