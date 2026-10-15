package browserctl

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// State is what one context (ContextKey) remembers between commands:
// ~/.monoagent/browser/sessions/<context-key>.json.
type State struct {
	// TabID and Instance are the last tab the context used and the browser
	// it lives in; commands without --tab act on it.
	TabID    int    `json:"tabId,omitempty"`
	Instance string `json:"instance,omitempty"`
	// Reported is, per tab ("<instance>/<tabId>"), the submit_guard seq of
	// the last blocked submit already turned into a HIL item.
	Reported  map[string]int `json:"reported,omitempty"`
	UpdatedAt string         `json:"updatedAt,omitempty"`
}

// StateDir is where the session state files live.
func StateDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".monoagent", "browser", "sessions"), nil
}

// LoadState reads key's state; a missing or unreadable file is an empty one.
func LoadState(key string) State {
	var st State
	dir, err := StateDir()
	if err != nil {
		return st
	}
	raw, err := os.ReadFile(filepath.Join(dir, safeKey(key)+".json"))
	if err != nil {
		return st
	}
	_ = json.Unmarshal(raw, &st)
	return st
}

// SaveState writes key's state (0600, replaced atomically).
func SaveState(key string, st State) error {
	dir, err := StateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, safeKey(key)+".json")
	tmp, err := os.CreateTemp(dir, ".state-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("saving browser session state: %w", err)
	}
	return nil
}

// tabKey keys State.Reported.
func tabKey(instance string, tabID int) string { return instance + "/" + strconv.Itoa(tabID) }
