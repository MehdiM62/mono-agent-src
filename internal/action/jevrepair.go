package action

// Selector repairs from Jev picks (jevfallback.go). When the Jev fallback
// finds the element of a package selector (a step whose configKey names an
// entry of the package's selectors.json), the picked element's fingerprint
// is read while its marker is still on it and converted into selector
// candidates, the same way `automation rerecord` converts a clicked
// element. The selector observer, when it implements JevRepairObserver,
// records them as a repair proposal (internal/automation/health_repair.go);
// nothing here changes the package.
//
// The label Jev chose is also handed to a JevHintStore, when the observer
// implements one, so the next pick's hint survives the process.

import (
	_ "embed"
	"encoding/json"
	"sort"
	"time"

	"github.com/monoes/mono-agent/internal/jevpick"
	"github.com/monoes/mono-agent/internal/recording"
)

// jevFingerprintJS is a function expression taking the marker.
//
//go:embed jev_fingerprint.js
var jevFingerprintJS string

// JevPick is one successful Jev pick of a package selector's element.
type JevPick struct {
	Label string        // the label Jev chose
	Entry SelectorEntry // candidates built from the picked element, best first
}

// JevRepairObserver is optionally implemented by a SelectorObserver that
// records Jev picks of package selectors as repair proposals. It must not
// block.
type JevRepairObserver interface {
	ObserveJevPick(automationID, key string, pick JevPick)
}

// JevHintStore is optionally implemented by a SelectorObserver that
// persists the label of the element Jev last chose per step (hintKey is
// "<platform>/<action>/<stepID>"). SaveJevHint must not block.
type JevHintStore interface {
	LoadJevHint(hintKey string) (label string, ok bool)
	SaveJevHint(hintKey, label string)
}

// captureJevRepair reads the fingerprint of the element carrying marker
// and reports it as a repair proposal for step's package selector. Best
// effort: any failure only means no proposal for this pick.
func (ae *ActionExecutor) captureJevRepair(step StepDef, page jevpick.Page, marker, label string) {
	obs, ok := ae.selObs.(JevRepairObserver)
	if !ok || ae.pkg == nil || step.ConfigKey == "" {
		return
	}
	cur, ok := ae.pkgSelectorEntry(step.ConfigKey)
	if !ok {
		return
	}
	markerJSON, _ := json.Marshal(marker)
	var fp *recording.Fingerprint
	if err := jevpick.NewBrowser(page).Evaluate("("+jevFingerprintJS+")("+string(markerJSON)+")", false, &fp); err != nil || fp == nil {
		ae.logger.Debug().Err(err).Str("stepID", step.ID).Msg("jev fallback: no fingerprint of the picked element")
		return
	}
	entry := SelectorEntryFromFingerprint(fp, cur.Intent, time.Now())
	if len(entry.Candidates) == 0 {
		return
	}
	obs.ObserveJevPick(ae.pkg.ID(), step.ConfigKey, JevPick{Label: label, Entry: entry})
}

// SelectorEntryFromFingerprint converts a picked element's candidates into
// a selector entry: unique ones first, each group by score (stable), intent
// kept, verifiedAt now. A sensitive field's text candidates are dropped.
// It is the conversion `automation rerecord` applies to a clicked element.
func SelectorEntryFromFingerprint(fp *recording.Fingerprint, intent string, now time.Time) SelectorEntry {
	e := SelectorEntry{Intent: intent, VerifiedAt: now.UTC().Format(time.RFC3339), Candidates: []SelectorCandidate{}}
	if fp == nil {
		return e
	}
	cands := append([]recording.Candidate(nil), fp.Candidates...)
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].Unique != cands[j].Unique {
			return cands[i].Unique
		}
		return cands[i].Score > cands[j].Score
	})
	for _, c := range cands {
		sc := SelectorCandidate{Score: c.Score}
		switch c.Kind {
		case "css":
			sc.CSS = c.Value
		case "xpath":
			sc.XPath = c.Value
		case "aria":
			if c.Role == "" {
				continue
			}
			sc.Aria = &AriaSelector{Role: c.Role, Name: c.Name}
		case "text":
			if fp.Sensitive {
				continue
			}
			sc.Text = c.Value
		default:
			continue
		}
		if sc.CSS == "" && sc.XPath == "" && sc.Aria == nil && sc.Text == "" {
			continue
		}
		e.Candidates = append(e.Candidates, sc)
	}
	return e
}
