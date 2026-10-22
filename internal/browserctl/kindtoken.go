package browserctl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// kindTokenMaxAge: an older record never counts, its process alive or not.
const kindTokenMaxAge = 7 * 24 * time.Hour

// kindRecord vouches for Kind while the process PID runs: the one that
// started at Start (processStart), so a pid the system reuses for another
// process doesn't count.
type kindRecord struct {
	Kind    string    `json:"kind"`
	PID     int       `json:"pid"`
	Start   string    `json:"start"`
	Created time.Time `json:"created"`
}

// recordFor is the record vouching for kind while process pid runs.
func recordFor(kind string, pid int, created time.Time) kindRecord {
	start, _ := processStart(pid)
	return kindRecord{Kind: kind, PID: pid, Start: start, Created: created}
}

// KindTokenDir is where the records live: ~/.monoagent/agent-kinds.
func KindTokenDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".monoagent", "agent-kinds"), nil
}

// IssueAgentKind vouches for kind (KindCoder, KindApply or KindOrg) for the agents
// this process starts: it writes the record (dir 0700, file 0600) and
// returns the environment to give them, MONOAGENT_AGENT_KIND and
// MONOAGENT_AGENT_KIND_TOKEN as "KEY=value". The record names this
// process, with its start time, and counts while that very process runs
// (not one the system later gives its pid), for at most seven days. A launcher that exits while
// what it started goes on (a detached org run) calls bind with the started
// process's pid once it has one: the record then counts while that process
// runs. Call done when the turn ends; it removes the record. When the
// record can't be written, env has no token (the agents run as kind other)
// and bind does nothing; any other kind gives no env.
func IssueAgentKind(kind string) (env []string, bind func(pid int), done func()) {
	bind, done = func(int) {}, func() {}
	if kind != KindCoder && kind != KindApply && kind != KindOrg {
		return nil, bind, done
	}
	env = []string{EnvAgentKind + "=" + kind}
	dir, err := KindTokenDir()
	if err != nil || os.MkdirAll(dir, 0o700) != nil || os.Chmod(dir, 0o700) != nil {
		return env, bind, done
	}
	pruneKindTokens(dir)
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return env, bind, done
	}
	token := hex.EncodeToString(b[:])
	path := filepath.Join(dir, token+".json")
	created := time.Now().UTC()
	if writeKindRecordFile(dir, path, recordFor(kind, os.Getpid(), created)) != nil {
		return env, bind, done
	}
	bind = func(pid int) {
		if pid > 0 {
			_ = writeKindRecordFile(dir, path, recordFor(kind, pid, created))
		}
	}
	return append(env, EnvAgentKindToken+"="+token), bind, func() { _ = os.Remove(path) }
}

// writeKindRecordFile writes rec to path aside and renames it into place,
// so no reader (or prune) sees it half done.
func writeKindRecordFile(dir, path string, rec kindRecord) error {
	raw, _ := json.Marshal(rec)
	f, err := os.CreateTemp(dir, ".new-*")
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(f.Name(), 0o600)
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
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

// live: the record is younger than kindTokenMaxAge, and the process it
// names runs and is the one that started when the record says (a pid the
// system reused for another process doesn't count).
func (r kindRecord) live() bool {
	if r.Created.IsZero() || time.Since(r.Created) >= kindTokenMaxAge || r.Start == "" {
		return false
	}
	start, ok := processStart(r.PID)
	return ok && start == r.Start
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
