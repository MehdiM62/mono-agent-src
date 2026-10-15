package automation

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"reflect"
	"strings"
	"time"

	"github.com/monoes/mono-agent/internal/action"
)

// Selector repairs from Jev picks. When the Jev fallback finds the element
// of a package selector (internal/action/jevrepair.go), the executor
// reports the candidates built from the picked element. The recorder keeps
// one proposal per (automation, selector key):
//
//   - a pick whose best candidate equals the proposal's counts as a
//     consistent pick (picks + 1); any other pick starts the proposal over
//     (picks = 1, its candidates);
//   - after RepairReadyPicks consistent picks the proposal is ready:
//     `automation doctor` lists it with its fix, `automation doctor
//     accept-repair <id> <key>` (AcceptRepair), which replaces the selector
//     like `automation rerecord` does;
//   - with the config automation.autoRepair on (`config set
//     automation.autoRepair true`; off by default), the recorder applies a
//     ready proposal itself.
//
// Applying a proposal resets the selector's health (ResetSelectorHealth),
// which also drops the proposal. The label Jev chose is kept per step as
// the next pick's hint (LoadJevHint / SaveJevHint).
//
// Proposals and hints live in the settings table (no schema change):
// repairSettingPrefix + "<automation>/<key>" holds a RepairProposal as
// JSON, jevHintSettingPrefix + "<platform>/<action>/<step>" a label.

const (
	// RepairReadyPicks is how many consistent Jev picks make a proposal ready.
	RepairReadyPicks = 2

	repairSettingPrefix  = "automation.jevRepair/"
	jevHintSettingPrefix = "automation.jevHint/"

	// AutoRepairConfig is the config (configs table, `config set`) that
	// lets the recorder apply ready proposals without asking.
	AutoRepairConfig = "automation.autoRepair"
)

// RepairProposal is a selector replacement proposed by Jev picks.
type RepairProposal struct {
	AutomationID string                     `json:"automationId"`
	Key          string                     `json:"key"`
	Label        string                     `json:"label,omitempty"`  // what Jev called the element
	Intent       string                     `json:"intent,omitempty"` // the selector's intent
	Candidates   []action.SelectorCandidate `json:"candidates"`
	Picks        int                        `json:"picks"` // consistent picks so far
	FirstSeen    time.Time                  `json:"firstSeen"`
	LastSeen     time.Time                  `json:"lastSeen"`
	Ready        bool                       `json:"ready"`
}

// Entry is the selector entry applying the proposal writes.
func (p RepairProposal) Entry(now time.Time) action.SelectorEntry {
	return action.SelectorEntry{Intent: p.Intent, VerifiedAt: now.UTC().Format(time.RFC3339),
		Candidates: append([]action.SelectorCandidate(nil), p.Candidates...)}
}

func repairSettingKey(id, key string) string { return repairSettingPrefix + id + "/" + key }

// LoadRepairProposals returns the proposals of automationID ("" = all),
// ordered by automation and key.
func LoadRepairProposals(db *sql.DB, automationID string) ([]RepairProposal, error) {
	rows, err := db.Query(`SELECT value FROM settings WHERE key >= ? AND key < ? ORDER BY key`,
		repairSettingPrefix, repairSettingPrefix+"\xff")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RepairProposal
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var p RepairProposal
		if json.Unmarshal([]byte(raw), &p) != nil || p.AutomationID == "" || p.Key == "" {
			continue
		}
		if automationID != "" && p.AutomationID != automationID {
			continue
		}
		p.Ready = p.Picks >= RepairReadyPicks
		out = append(out, p)
	}
	return out, rows.Err()
}

// LoadRepairProposal returns the proposal for id/key; ok is false when
// there is none.
func LoadRepairProposal(db *sql.DB, id, key string) (RepairProposal, bool, error) {
	var raw string
	err := db.QueryRow(`SELECT value FROM settings WHERE key = ?`, repairSettingKey(id, key)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return RepairProposal{}, false, nil
	}
	if err != nil {
		return RepairProposal{}, false, err
	}
	var p RepairProposal
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return RepairProposal{}, false, fmt.Errorf("repair proposal %s/%s: %w", id, key, err)
	}
	p.Ready = p.Picks >= RepairReadyPicks
	return p, true, nil
}

// deleteRepairProposal drops the proposal for id/key, if any.
func deleteRepairProposal(db *sql.DB, id, key string) error {
	_, err := db.Exec(`DELETE FROM settings WHERE key = ?`, repairSettingKey(id, key))
	return err
}

// recordRepairPick folds one Jev pick into the proposal for id/key and
// returns the updated proposal.
func recordRepairPick(db *sql.DB, id, key string, pick action.JevPick, at time.Time) (RepairProposal, error) {
	if len(pick.Entry.Candidates) == 0 {
		return RepairProposal{}, errors.New("pick without candidates")
	}
	p, found, err := LoadRepairProposal(db, id, key)
	if err != nil {
		// An unreadable proposal is replaced by this pick.
		found = false
	}
	if found && len(p.Candidates) > 0 && reflect.DeepEqual(p.Candidates[0], pick.Entry.Candidates[0]) {
		p.Picks++
	} else {
		p = RepairProposal{AutomationID: id, Key: key, Picks: 1, FirstSeen: at.UTC()}
	}
	p.Label, p.Intent = pick.Label, pick.Entry.Intent
	p.Candidates = pick.Entry.Candidates
	p.LastSeen = at.UTC()
	p.Ready = p.Picks >= RepairReadyPicks
	raw, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	_, err = db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, repairSettingKey(id, key), string(raw))
	return p, err
}

// SelectorReplacer replaces a selector entry (Registry.ReplaceSelector).
type SelectorReplacer interface {
	ReplaceSelector(id, key string, e action.SelectorEntry) (string, error)
}

// AcceptedRepair is the result of applying a proposal.
type AcceptedRepair struct {
	Automation string                     `json:"automation"`
	Key        string                     `json:"key"`
	Where      string                     `json:"where"` // package | overlay
	Candidates []action.SelectorCandidate `json:"candidates"`
	Label      string                     `json:"label,omitempty"`
	Picks      int                        `json:"picks"`
}

// ErrNoRepairProposal means there is no proposal for the selector.
var ErrNoRepairProposal = errors.New("no repair proposal for this selector")

// AcceptRepair applies the proposal for id/key through rep, as `automation
// rerecord` applies a clicked element, then starts the selector's health
// over (which drops the proposal).
func AcceptRepair(db *sql.DB, rep SelectorReplacer, id, key string, now time.Time) (*AcceptedRepair, error) {
	p, found, err := LoadRepairProposal(db, id, key)
	if err != nil {
		return nil, err
	}
	if !found || len(p.Candidates) == 0 {
		return nil, fmt.Errorf("%w: %s %s", ErrNoRepairProposal, id, key)
	}
	where, err := rep.ReplaceSelector(id, key, p.Entry(now))
	if err != nil {
		return nil, err
	}
	res := &AcceptedRepair{Automation: id, Key: key, Where: where, Candidates: p.Candidates, Label: p.Label, Picks: p.Picks}
	if err := ResetSelectorHealth(db, id, key); err != nil {
		return res, fmt.Errorf("selector replaced, but its health was not reset: %w", err)
	}
	return res, nil
}

// AutoRepairEnabled reports whether the automation.autoRepair config is on.
func AutoRepairEnabled(db *sql.DB) bool {
	var v string
	if err := db.QueryRow(`SELECT config_data FROM configs WHERE name = ?`, AutoRepairConfig).Scan(&v); err != nil {
		return false
	}
	switch strings.ToLower(strings.Trim(strings.TrimSpace(v), `"`)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// --- the recorder side ---------------------------------------------------

// jevEvent is a Jev pick or a hint save, queued with the observations.
type jevEvent struct {
	healthKey // pick: the selector
	pick      *action.JevPick
	hintKey   string // hint save
	label     string
	at        time.Time
}

var (
	_ action.JevRepairObserver = (*HealthRecorder)(nil)
	_ action.JevHintStore      = (*HealthRecorder)(nil)
)

// ObserveJevPick implements action.JevRepairObserver. It never blocks.
func (r *HealthRecorder) ObserveJevPick(automationID, key string, pick action.JevPick) {
	if r == nil || automationID == "" || key == "" || len(pick.Entry.Candidates) == 0 {
		return
	}
	p := pick
	p.Entry.Candidates = append([]action.SelectorCandidate(nil), pick.Entry.Candidates...)
	r.enqueueJev(&jevEvent{healthKey: healthKey{automationID, key}, pick: &p, at: r.now()})
}

// SaveJevHint implements action.JevHintStore. It never blocks.
func (r *HealthRecorder) SaveJevHint(hintKey, label string) {
	if r == nil || hintKey == "" || label == "" {
		return
	}
	r.enqueueJev(&jevEvent{hintKey: hintKey, label: label, at: r.now()})
}

// LoadJevHint implements action.JevHintStore (one small read; the
// executor calls it only on the Jev path, once per step and process).
func (r *HealthRecorder) LoadJevHint(hintKey string) (string, bool) {
	if r == nil || hintKey == "" {
		return "", false
	}
	var label string
	if err := r.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, jevHintSettingPrefix+hintKey).Scan(&label); err != nil {
		return "", false
	}
	return label, label != ""
}

func (r *HealthRecorder) enqueueJev(ev *jevEvent) {
	if r.closed.Load() {
		r.dropped.Add(1)
		return
	}
	select {
	case r.ch <- healthObservation{jev: ev}:
	default:
		r.dropped.Add(1)
	}
}

// applyJevEvents writes queued picks and hints, in order, and applies the
// proposals that became ready when auto-repair is on. Loop goroutine only.
func (r *HealthRecorder) applyJevEvents(events []*jevEvent) {
	if len(events) == 0 {
		return
	}
	latest := map[healthKey]RepairProposal{}
	var order []healthKey
	for _, ev := range events {
		if ev.pick == nil {
			if _, err := r.db.Exec(`INSERT INTO settings (key, value) VALUES (?, ?)
				ON CONFLICT(key) DO UPDATE SET value = excluded.value`, jevHintSettingPrefix+ev.hintKey, ev.label); err != nil {
				log.Printf("automation: jev hint %s: %v", ev.hintKey, err)
			}
			continue
		}
		p, err := recordRepairPick(r.db, ev.id, ev.key, *ev.pick, ev.at)
		if err != nil {
			log.Printf("automation: selector repair %s/%s: %v", ev.id, ev.key, err)
			continue
		}
		if _, seen := latest[ev.healthKey]; !seen {
			order = append(order, ev.healthKey)
		}
		latest[ev.healthKey] = p
	}
	if r.opts.Repairer == nil {
		return
	}
	auto := r.opts.AutoRepair
	if auto == nil {
		auto = AutoRepairEnabled
	}
	checked, enabled := false, false
	for _, k := range order {
		p := latest[k]
		if !p.Ready {
			continue
		}
		if !checked {
			checked, enabled = true, auto(r.db)
		}
		if !enabled {
			return
		}
		if _, err := AcceptRepair(r.db, r.opts.Repairer, p.AutomationID, p.Key, r.now()); err != nil {
			log.Printf("automation: auto-repair %s/%s: %v", p.AutomationID, p.Key, err)
			continue
		}
		log.Printf("automation: auto-repaired selector %s/%s from %d consistent Jev picks", p.AutomationID, p.Key, p.Picks)
	}
}

// bootedReplacer replaces through the booted registry (resolved at call time).
type bootedReplacer struct{}

func (bootedReplacer) ReplaceSelector(id, key string, e action.SelectorEntry) (string, error) {
	reg := bootedRegistry()
	if reg == nil {
		return "", errors.New("no automation registry booted")
	}
	return reg.ReplaceSelector(id, key, e)
}
