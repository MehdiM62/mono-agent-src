package extension

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Per-browser pairing tokens, minted by the zero-step claim flow
// (autopair.go). Each browser that claims gets its own token, recorded here
// with a label so it can be listed and revoked on its own
// (`extension browsers list|revoke`) without resetting the shared
// extension.token every other browser and CLI process uses.
//
// Only a SHA-256 of each token is stored: the file says who is paired, it
// cannot be used to connect. The bridge and the CLI are different processes,
// so the file is the source of truth and every check reads it; a revocation
// takes effect at the next handshake and, for a socket that is already open,
// at the next sweepRevoked (every ping interval).

// maxPairedBrowsers bounds the file. A browser that claims past it replaces
// the oldest entry, so a loop of claims cannot grow the file without limit.
const maxPairedBrowsers = 32

// PairedBrowser is one browser the claim flow has paired.
type PairedBrowser struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	Origin    string    `json:"origin"`
	Agent     string    `json:"agent,omitempty"`
	Instance  string    `json:"instance,omitempty"`
	TokenHash string    `json:"tokenHash"`
	PairedAt  time.Time `json:"pairedAt"`
}

// ErrNoSuchBrowser means a revoke named no paired browser (or several).
var ErrNoSuchBrowser = errors.New("no such paired browser")

var browsersMu sync.Mutex

func browsersPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".monoagent", "extension.browsers.json"), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func readBrowsersLocked() ([]PairedBrowser, error) {
	path, err := browsersPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read paired browsers: %w", err)
	}
	var list []PairedBrowser
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("paired browsers file is unreadable: %w", err)
	}
	return list, nil
}

func writeBrowsersLocked(list []PairedBrowser) error {
	path, err := browsersPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write paired browsers: %w", err)
	}
	return os.Rename(tmp, path)
}

// ListPairedBrowsers returns the browsers the claim flow has paired, oldest first.
func ListPairedBrowsers() ([]PairedBrowser, error) {
	browsersMu.Lock()
	defer browsersMu.Unlock()
	list, err := readBrowsersLocked()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].PairedAt.Before(list[j].PairedAt) })
	return list, nil
}

// RevokePairedBrowser removes one paired browser, named by its id or a unique
// prefix of it (4+ characters). Its token stops working at once for new
// connections.
func RevokePairedBrowser(idOrPrefix string) (PairedBrowser, error) {
	idOrPrefix = strings.TrimSpace(idOrPrefix)
	if len(idOrPrefix) < 4 {
		return PairedBrowser{}, fmt.Errorf("%w: give at least 4 characters of the id", ErrNoSuchBrowser)
	}
	browsersMu.Lock()
	defer browsersMu.Unlock()
	list, err := readBrowsersLocked()
	if err != nil {
		return PairedBrowser{}, err
	}
	hit := -1
	for i, b := range list {
		if b.ID == idOrPrefix {
			hit = i
			break
		}
		if strings.HasPrefix(b.ID, idOrPrefix) {
			if hit != -1 {
				return PairedBrowser{}, fmt.Errorf("%w: %q matches more than one browser", ErrNoSuchBrowser, idOrPrefix)
			}
			hit = i
		}
	}
	if hit == -1 {
		return PairedBrowser{}, fmt.Errorf("%w: %q", ErrNoSuchBrowser, idOrPrefix)
	}
	gone := list[hit]
	list = append(list[:hit], list[hit+1:]...)
	if err := writeBrowsersLocked(list); err != nil {
		return PairedBrowser{}, err
	}
	return gone, nil
}

// addPairedBrowser mints a token for b, replacing any earlier entry of the
// same instance (a browser re-claiming gets a new token and the old one dies).
func addPairedBrowser(b PairedBrowser) (token string, _ PairedBrowser, _ error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", PairedBrowser{}, fmt.Errorf("generate token: %w", err)
	}
	idBuf := make([]byte, 8)
	if _, err := rand.Read(idBuf); err != nil {
		return "", PairedBrowser{}, fmt.Errorf("generate id: %w", err)
	}
	token = hex.EncodeToString(buf)
	b.ID = hex.EncodeToString(idBuf)
	b.TokenHash = hashToken(token)
	b.PairedAt = time.Now().UTC()

	browsersMu.Lock()
	defer browsersMu.Unlock()
	list, err := readBrowsersLocked()
	if err != nil {
		return "", PairedBrowser{}, err
	}
	if b.Instance != "" {
		kept := list[:0]
		for _, e := range list {
			if e.Instance != b.Instance {
				kept = append(kept, e)
			}
		}
		list = kept
	}
	list = append(list, b)
	if len(list) > maxPairedBrowsers {
		sort.SliceStable(list, func(i, j int) bool { return list[i].PairedAt.Before(list[j].PairedAt) })
		list = list[len(list)-maxPairedBrowsers:]
	}
	if err := writeBrowsersLocked(list); err != nil {
		return "", PairedBrowser{}, err
	}
	return token, b, nil
}

// pairedBrowserForToken finds the entry a presented token belongs to,
// comparing hashes in constant time.
func pairedBrowserForToken(token string) (PairedBrowser, bool) {
	if token == "" {
		return PairedBrowser{}, false
	}
	want := hashToken(token)
	browsersMu.Lock()
	defer browsersMu.Unlock()
	list, err := readBrowsersLocked()
	if err != nil {
		return PairedBrowser{}, false
	}
	found := PairedBrowser{}
	ok := false
	for _, e := range list {
		if subtle.ConstantTimeCompare([]byte(e.TokenHash), []byte(want)) == 1 {
			found, ok = e, true
		}
	}
	return found, ok
}

// pairedBrowserIDs is the set of ids still paired.
func pairedBrowserIDs() map[string]bool {
	browsersMu.Lock()
	defer browsersMu.Unlock()
	list, _ := readBrowsersLocked()
	out := make(map[string]bool, len(list))
	for _, e := range list {
		out[e.ID] = true
	}
	return out
}
