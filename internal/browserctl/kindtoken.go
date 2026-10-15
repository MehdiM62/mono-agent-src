package browserctl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/daemonhb"
)

// Agent kind tokens (plan C9 item 3). A declared kind (MONOAGENT_AGENT_KIND,
// MONOMIND_ORG_ROLE) is an environment variable any shell can set to borrow
// another kind's level, so it counts only with a second signal:
// MONOAGENT_AGENT_KIND_TOKEN naming a record the spawner wrote for that kind
// under ~/.monoagent/agent-kinds and removes when the turn ends. A shell
// without a valid one is kind other. An agent that reads another process's
// environment (or writes such a record itself) can still borrow its kind:
// like the rest of the policy, this is a courtesy layer, not a sandbox.

// EnvAgentKindToken names the record that vouches for EnvAgentKind.
const EnvAgentKindToken = "MONOAGENT_AGENT_KIND_TOKEN"

// kindTokenMaxAge is how long a record with no spawner pid counts.
const kindTokenMaxAge = 24 * time.Hour

type kindRecord struct {
	Kind    string    `json:"kind"`
	PID     int       `json:"pid"`
	Created time.Time `json:"created"`
}

// KindTokenDir is where the records live: ~/.monoagent/agent-kinds.
func KindTokenDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".monoagent", "agent-kinds"), nil
}

// IssueAgentKind vouches for kind (KindCoder or KindOrg) for the agents
// this process starts: it writes the record (dir 0700, file 0600) and
// returns the environment to give them, MONOAGENT_AGENT_KIND and
// MONOAGENT_AGENT_KIND_TOKEN as "KEY=value". Call done when the turn ends;
// it removes the record. The record names this process, so it also stops
// counting when this process is gone. When it can't be written, env has no
// token and the agents run as kind other; any other kind gives no env.
func IssueAgentKind(kind string) (env []string, done func()) {
	done = func() {}
	if kind != KindCoder && kind != KindOrg {
		return nil, done
	}
	env = []string{EnvAgentKind + "=" + kind}
	dir, err := KindTokenDir()
	if err != nil || os.MkdirAll(dir, 0o700) != nil || os.Chmod(dir, 0o700) != nil {
		return env, done
	}
	pruneKindTokens(dir)
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return env, done
	}
	token := hex.EncodeToString(b[:])
	raw, _ := json.Marshal(kindRecord{Kind: kind, PID: os.Getpid(), Created: time.Now().UTC()})
	// Written aside and renamed, so no reader (or prune) sees it half done.
	f, err := os.CreateTemp(dir, ".new-*")
	if err != nil {
		return env, done
	}
	_, werr := f.Write(raw)
	cerr := f.Close()
	path := filepath.Join(dir, token+".json")
	if werr != nil || cerr != nil || os.Chmod(f.Name(), 0o600) != nil || os.Rename(f.Name(), path) != nil {
		_ = os.Remove(f.Name())
		return env, done
	}
	return append(env, EnvAgentKindToken+"="+token), func() { _ = os.Remove(path) }
}

// kindVouched reports whether this process's MONOAGENT_AGENT_KIND_TOKEN
// names a live record for kind.
func kindVouched(kind string) bool {
	token := strings.TrimSpace(os.Getenv(EnvAgentKindToken))
	if !validToken(token) {
		return false
	}
	dir, err := KindTokenDir()
	if err != nil {
		return false
	}
	rec, ok := readKindRecord(filepath.Join(dir, token+".json"))
	return ok && rec.Kind == kind && rec.live()
}

// live: the spawner still runs, or (no pid recorded) the record is younger
// than kindTokenMaxAge.
func (r kindRecord) live() bool {
	if r.PID > 0 {
		return daemonhb.ProcessAlive(r.PID)
	}
	return !r.Created.IsZero() && time.Since(r.Created) < kindTokenMaxAge
}

func readKindRecord(path string) (kindRecord, bool) {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() {
		return kindRecord{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return kindRecord{}, false
	}
	var rec kindRecord
	return rec, json.Unmarshal(raw, &rec) == nil
}

// validToken: 32 lowercase hex digits, so a token never names a path
// outside the directory.
func validToken(t string) bool {
	if len(t) != 32 {
		return false
	}
	for _, r := range t {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// pruneKindTokens removes the records that no longer count, left by a
// spawner that ended without calling done.
func pruneKindTokens(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") || !validToken(strings.TrimSuffix(name, ".json")) {
			continue
		}
		path := filepath.Join(dir, name)
		if rec, ok := readKindRecord(path); !ok || !rec.live() {
			_ = os.Remove(path)
		}
	}
}
