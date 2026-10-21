package extension

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
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

	"github.com/google/uuid"
)

// Pending pairing claims (`extension.autoPair` = ask).
//
// A claim from a browser extension is not a credential: its Origin header is
// a string any local process can send. In ask mode the bridge therefore only
// records a PENDING claim here and answers with a claim id and a poll secret;
// a token exists only after the owner approves the claim through a channel a
// browser or a script cannot reach (`monoagentcli extension approve`, or the
// card in the Human in Loop queue, which decides through the same code). The
// extension polls with the secret; the poll that sees "approved" mints the
// token once and deletes the record.
//
// The file is shared by the bridge and the CLI (different processes) and
// holds only a SHA-256 of the poll secret, never a token.

// Claim states.
const (
	ClaimPending  = "pending"
	ClaimApproved = "approved"
	ClaimDenied   = "denied"
)

const (
	// PendingClaimTTL is how long a claim waits for the owner.
	PendingClaimTTL = 2 * time.Minute
	// maxPendingClaims and maxPendingPerOrigin bound the file and the owner's
	// queue. A full queue refuses new claims; it never drops a waiting one.
	maxPendingClaims    = 5
	maxPendingPerOrigin = 2
	// PairCardKind marks a HIL item that is a pairing claim.
	PairCardKind = "extension.pair"
	// PairClaimWording is what the card tells the owner they are granting.
	PairClaimWording = "claims access to your documents, vault, tasks and page control"
)

var (
	// ErrClaimsFull: no free slot for a new pending claim.
	ErrClaimsFull = errors.New("too many pairing requests are waiting for approval; approve or deny them, or wait for them to expire")
	// ErrNoSuchClaim: no pending claim by that id (or it expired).
	ErrNoSuchClaim = errors.New("no such pending pairing claim")
	// ErrClaimExpired: the claim waited longer than PendingClaimTTL.
	ErrClaimExpired = errors.New("this pairing claim expired")
)

// PendingClaim is one browser waiting for the owner.
type PendingClaim struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Origin     string    `json:"origin"`
	Agent      string    `json:"agent,omitempty"`
	Instance   string    `json:"instance,omitempty"`
	SecretHash string    `json:"secretHash"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

var pendingMu sync.Mutex

func pendingPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".monoagent", "extension.pending.json"), nil
}

func readPendingLocked() ([]PendingClaim, error) {
	path, err := pendingPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pending claims: %w", err)
	}
	var list []PendingClaim
	if err := json.Unmarshal(data, &list); err != nil {
		// A damaged file holds nothing the owner approved: start over.
		return nil, nil
	}
	return list, nil
}

func writePendingLocked(list []PendingClaim) error {
	path, err := pendingPath()
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
		return fmt.Errorf("write pending claims: %w", err)
	}
	return os.Rename(tmp, path)
}

// liveOnly drops expired claims, returning them separately.
func liveOnly(list []PendingClaim, now time.Time) (live, gone []PendingClaim) {
	for _, c := range list {
		if now.After(c.ExpiresAt) {
			gone = append(gone, c)
		} else {
			live = append(live, c)
		}
	}
	return live, gone
}

// CreatePendingClaim records a pending claim and returns it with its poll
// secret (32 random bytes, hex), which is not stored. It refuses, and drops
// nothing, when the queue or the origin's share of it is full.
func CreatePendingClaim(c PendingClaim) (PendingClaim, string, error) {
	sec := make([]byte, 32)
	idb := make([]byte, 8)
	if _, err := rand.Read(sec); err != nil {
		return PendingClaim{}, "", err
	}
	if _, err := rand.Read(idb); err != nil {
		return PendingClaim{}, "", err
	}
	secret := hex.EncodeToString(sec)
	now := time.Now().UTC()
	c.ID, c.SecretHash, c.Status = hex.EncodeToString(idb), hashToken(secret), ClaimPending
	c.Label, c.Origin, c.Agent = clip(c.Label), clip(c.Origin), clip(c.Agent)
	c.CreatedAt, c.ExpiresAt = now, now.Add(PendingClaimTTL)

	pendingMu.Lock()
	defer pendingMu.Unlock()
	list, err := readPendingLocked()
	if err != nil {
		return PendingClaim{}, "", err
	}
	list, _ = liveOnly(list, now)
	perOrigin := 0
	for _, e := range list {
		if e.Origin == c.Origin {
			perOrigin++
		}
	}
	if len(list) >= maxPendingClaims || perOrigin >= maxPendingPerOrigin {
		return PendingClaim{}, "", ErrClaimsFull
	}
	if err := writePendingLocked(append(list, c)); err != nil {
		return PendingClaim{}, "", err
	}
	return c, secret, nil
}

// ListPendingClaims returns the claims still waiting, oldest first.
func ListPendingClaims() ([]PendingClaim, error) {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	list, err := readPendingLocked()
	if err != nil {
		return nil, err
	}
	list, _ = liveOnly(list, time.Now())
	var out []PendingClaim
	for _, c := range list {
		if c.Status == ClaimPending {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// DecidePendingClaim approves or denies one waiting claim, named by its id, a
// unique prefix of it (4+ characters), or "" for the newest. It is the only
// way a claim becomes approved, and runs only from the owner's channels.
func DecidePendingClaim(idOrPrefix string, approve bool) (PendingClaim, error) {
	idOrPrefix = strings.TrimSpace(idOrPrefix)
	if idOrPrefix != "" && len(idOrPrefix) < 4 {
		return PendingClaim{}, fmt.Errorf("%w: give at least 4 characters of the id", ErrNoSuchClaim)
	}
	now := time.Now()
	pendingMu.Lock()
	defer pendingMu.Unlock()
	list, err := readPendingLocked()
	if err != nil {
		return PendingClaim{}, err
	}
	hit := -1
	for i, c := range list {
		if c.Status != ClaimPending {
			continue
		}
		switch {
		case idOrPrefix == "":
			if hit == -1 || c.CreatedAt.After(list[hit].CreatedAt) {
				hit = i
			}
		case c.ID == idOrPrefix:
			hit = i
		case strings.HasPrefix(c.ID, idOrPrefix):
			if hit != -1 && list[hit].ID != idOrPrefix {
				return PendingClaim{}, fmt.Errorf("%w: %q matches more than one claim", ErrNoSuchClaim, idOrPrefix)
			}
			if hit == -1 {
				hit = i
			}
		}
	}
	if hit == -1 {
		return PendingClaim{}, fmt.Errorf("%w: %q", ErrNoSuchClaim, idOrPrefix)
	}
	if now.After(list[hit].ExpiresAt) {
		return list[hit], ErrClaimExpired
	}
	if approve {
		list[hit].Status = ClaimApproved
	} else {
		list[hit].Status = ClaimDenied
	}
	if err := writePendingLocked(list); err != nil {
		return PendingClaim{}, err
	}
	return list[hit], nil
}

// PollOutcome is what a poll of a claim learns.
type PollOutcome struct {
	State string // pending | approved | denied | expired
	Token string // set once, on approved
	Entry PairedBrowser
	Claim PendingClaim
}

// pollClaim checks the claim named by id with its secret (and, when the request
// carried one, the Origin the claim was made from). A wrong id and a
// wrong secret are the same ErrNoSuchClaim. A denied or expired claim is
// deleted as it is reported; an approved one mints its token here, once, and
// is deleted.
func pollClaim(id, secret, origin string) (PollOutcome, error) {
	now := time.Now()
	pendingMu.Lock()
	defer pendingMu.Unlock()
	list, err := readPendingLocked()
	if err != nil {
		return PollOutcome{}, err
	}
	at := -1
	want := hashToken(secret)
	for i, c := range list {
		if c.ID == id && subtle.ConstantTimeCompare([]byte(c.SecretHash), []byte(want)) == 1 && (origin == "" || c.Origin == origin) {
			at = i
		}
	}
	if secret == "" || at == -1 {
		return PollOutcome{}, ErrNoSuchClaim
	}
	c := list[at]
	drop := func() error { return writePendingLocked(append(list[:at:at], list[at+1:]...)) }
	switch {
	case now.After(c.ExpiresAt):
		return PollOutcome{State: "expired", Claim: c}, drop()
	case c.Status == ClaimDenied:
		return PollOutcome{State: ClaimDenied, Claim: c}, drop()
	case c.Status != ClaimApproved:
		return PollOutcome{State: ClaimPending, Claim: c}, nil
	}
	token, entry, err := addPairedBrowser(PairedBrowser{
		Label: c.Label, Origin: c.Origin, Agent: c.Agent, Instance: c.Instance,
	}, mintOpts{ID: c.ID, ReplaceInstance: true})
	if err != nil {
		if errors.Is(err, ErrBrowserLimit) {
			// Nothing was minted: keep the claim approved until it expires, so
			// room made by a revoke lets the next poll succeed.
			return PollOutcome{State: ClaimPending, Claim: c}, err
		}
		_ = drop()
		return PollOutcome{}, err
	}
	if err := drop(); err != nil {
		return PollOutcome{}, err
	}
	return PollOutcome{State: ClaimApproved, Token: token, Entry: entry, Claim: c}, nil
}

// expiredClaimIDs removes expired claims and returns their ids.
func expiredClaimIDs() []string {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	list, err := readPendingLocked()
	if err != nil || len(list) == 0 {
		return nil
	}
	live, gone := liveOnly(list, time.Now())
	if len(gone) == 0 {
		return nil
	}
	_ = writePendingLocked(live)
	ids := make([]string, len(gone))
	for i, c := range gone {
		ids[i] = c.ID
	}
	return ids
}

// PairCards puts a pending claim in front of the owner as a card in the Human
// in Loop queue and closes it again. A nil PairCards means no card (the CLI
// still decides).
type PairCards interface {
	Open(ctx context.Context, c PendingClaim)
	Settle(ctx context.Context, claimID, status string)
}

// IsPairCard reports whether a hil_pending node_config is a pairing card's,
// and which claim it names.
func IsPairCard(nodeConfig string) (claimID string, ok bool) {
	var r struct {
		Kind  string `json:"kind"`
		Claim string `json:"claim"`
	}
	if json.Unmarshal([]byte(nodeConfig), &r) != nil || r.Kind != PairCardKind || r.Claim == "" {
		return "", false
	}
	return r.Claim, true
}

// OpenPairCard inserts the card for c into one profile's queue.
func OpenPairCard(ctx context.Context, db *sql.DB, profileID string, c PendingClaim) error {
	ro, _ := json.Marshal(map[string]string{
		"claim": c.ID, "browser": c.Label, "origin": c.Origin, "instance": c.Instance,
		"expiresAt": c.ExpiresAt.Format(time.RFC3339),
		"summary":   fmt.Sprintf("The browser %q (%s) %s. Approve only if you just installed or opened it.", c.Label, c.Origin, PairClaimWording),
	})
	nc, _ := json.Marshal(map[string]string{"kind": PairCardKind, "claim": c.ID})
	_, err := db.ExecContext(ctx,
		`INSERT INTO hil_pending (id, execution_id, workflow_id, node_id, node_name, status, readonly_data, editable_data, node_config, profile_id)
		 VALUES (?, ?, ?, ?, ?, 'pending', ?, '{}', ?, ?)`,
		uuid.NewString(), "extpair:"+c.ID, PairCardKind, PairCardKind, "Pair browser: "+c.Label, string(ro), string(nc), profileID)
	return err
}

// SettlePairCards closes every card of a claim, in every profile. status is
// "approved", "rejected" or "expired".
func SettlePairCards(ctx context.Context, db *sql.DB, claimID, status string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE hil_pending SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE execution_id = ? AND status = 'pending'`,
		status, "extpair:"+claimID)
	return err
}
