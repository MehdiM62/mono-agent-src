package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/monoes/mono-agent/internal/ai"
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
	logPath := filepath.Join(l.logDir, turnID+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	offset, _ := logFile.Seek(0, io.SeekEnd)
	// The prompt goes in through stdin from a private file: it is in no
	// process listing, and a 256 KiB message does not hit the argument limit.
	promptPath := filepath.Join(l.logDir, turnID+".prompt")
	if err := os.WriteFile(promptPath, []byte(message), 0o600); err != nil {
		return nil, err
	}
	prompt, err := os.Open(promptPath)
	if err != nil {
		return nil, err
	}
	defer prompt.Close()
	rest := []string{"chat", "--conversation", conversationID, "--turn", turnID, "--instance", l.instance, "--prompt-stdin"}
	for _, a := range attachments {
		rest = append(rest, "--attach="+a)
	}
	cmd := exec.Command(l.bin, l.args(profileID, rest...)...)
	cmd.Dir = filepath.Dir(l.logDir)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = prompt, logFile, logFile
	if err := clicore.DetachStart(cmd); err != nil {
		os.Remove(promptPath)
		return nil, err
	}
	t := &execTurn{pid: cmd.Process.Pid, stamp: pidStamp(cmd.Process.Pid), done: make(chan struct{})}
	go func() {
		t.err = cmd.Wait()
		os.Remove(promptPath) // before done closes: whoever sees the turn end sees no prompt left
		close(t.done)
	}()
	if err := awaitAdmission(logPath, offset, t.done); err != nil {
		t.Stop()
		return nil, err
	}
	return t, nil
}

// admissionWait is how long a started turn gets to admit itself.
var admissionWait = 20 * time.Second

// awaitAdmission reads the turn process's first JSON line from its log: the
// admission, or the {"error"} of a turn refused before it was admitted (a
// conversation that does not exist, one the app is running a turn in, a turn
// id used before). A refusal comes back as the error, so the command fails
// with the reason instead of reading "done".
func awaitAdmission(logPath string, offset int64, done <-chan struct{}) error {
	deadline := time.Now().Add(admissionWait)
	for {
		exited := false
		select {
		case <-done:
			exited = true
		default:
		}
		raw, _ := os.ReadFile(logPath)
		if int64(len(raw)) > offset {
			raw = raw[offset:]
		} else {
			raw = nil
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.HasPrefix(line, "{") {
				continue
			}
			var m struct {
				Admitted bool   `json:"admitted"`
				Existed  bool   `json:"existed"`
				Error    string `json:"error"`
			}
			if json.Unmarshal([]byte(line), &m) != nil {
				continue
			}
			switch {
			case m.Error != "":
				return errors.New(m.Error)
			case m.Existed:
				return errors.New("this turn id was already used")
			case m.Admitted:
				return nil
			}
		}
		if exited {
			last := ""
			for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
				if strings.TrimSpace(line) != "" {
					last = strings.TrimSpace(line)
				}
			}
			if last == "" {
				last = "no output"
			}
			return fmt.Errorf("the turn process exited before the turn was admitted: %s", last)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the turn was not admitted within %s", admissionWait)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func (l *execLauncher) Adopt(pid int, stamp string) coderhost.Turn {
	// Without a start time there is nothing to tell this process from a later
	// one that reused the pid, and the turn would be signalled by pid: not adopted.
	if pid <= 1 || stamp == "" || !daemonhb.ProcessAlive(pid) || pidStamp(pid) != stamp {
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
func (t *execTurn) Stop()         { t.once.Do(func() { stopGroup(t.pid, "") }) }

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
	return !daemonhb.ProcessAlive(t.pid) || pidStamp(t.pid) != t.stamp
}

func (t *adoptedTurn) Wait() error {
	for !t.gone() {
		time.Sleep(500 * time.Millisecond)
	}
	return nil
}

func (t *adoptedTurn) Stop() {
	t.once.Do(func() { stopGroup(t.pid, t.stamp) })
}

// signalTurn sends sig to the turn's process group, if pid still is the
// session leader the host started. stamp, when set, is the start time pid must
// still have (a turn the host did not start itself); "" for a child of this
// process, whose pid cannot be reused while it is unreaped or its group lives.
type groupSignal func(pid int) error

func signalTurn(pid int, stamp string, sig groupSignal) {
	if pid <= 1 || (stamp != "" && pidStamp(pid) != stamp) {
		return
	}
	_ = sig(pid)
}

// stopGroup asks the turn's process group to end and, once hostTurnGrace has
// passed, kills whatever of the group is left, also when only its leader
// exited. It returns at once.
func stopGroup(pid int, stamp string) {
	signalTurn(pid, stamp, terminateGroup)
	time.AfterFunc(hostTurnGrace, func() { signalTurn(pid, stamp, killGroup) })
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

// hostHeldTurns is the reconcile sweep's skip list: a turn that is the current
// turn of a host session and whose process is still the one the host started
// (alive, same start time). A turn whose process is gone is not held, however
// the host ended, so it is interrupted like any other orphan.
func hostHeldTurns(cfg *Deps) func(ai.Turn) bool {
	db, err := cfg.OpenDB()
	if err != nil {
		return nil
	}
	defer db.Close()
	sessions, err := coderhost.NewStore(db.DB).ListSessions("")
	if err != nil {
		return nil
	}
	held := map[string]coderhost.Session{}
	for _, x := range sessions {
		if x.TurnID != "" {
			held[x.ProfileID+"\x00"+x.ConversationID+"\x00"+x.TurnID] = x
		}
	}
	return func(t ai.Turn) bool {
		x, ok := held[t.ProfileID+"\x00"+t.ConversationID+"\x00"+t.ID]
		return ok && x.TurnPID > 1 && x.TurnStamp != "" && daemonhb.ProcessAlive(x.TurnPID) && pidStamp(x.TurnPID) == x.TurnStamp
	}
}
