package action

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monoes/mono-agent/internal/jev/jevtest"
	"github.com/monoes/mono-agent/internal/recording"
)

// fpPage answers the fingerprint evaluation (jev_fingerprint.js) with a
// fixed fingerprint; everything else goes to the jevpick fake.
type fpPage struct {
	*jevFakePage
	fp      map[string]any
	fpCalls int
}

func (f *fpPage) CDP(method string, params map[string]interface{}) (map[string]interface{}, error) {
	if expr, _ := params["expression"].(string); method == "Runtime.evaluate" && strings.Contains(expr, "SECRET_WORDS") {
		f.fpCalls++
		return map[string]interface{}{"result": map[string]interface{}{"value": f.fp}}, nil
	}
	return f.jevFakePage.CDP(method, params)
}

var sendFingerprint = map[string]any{
	"tag": "button", "role": "button", "ariaName": "Send message",
	"candidates": []any{
		map[string]any{"kind": "css", "value": "main > button", "unique": true, "count": 1, "score": 0.35},
		map[string]any{"kind": "aria", "role": "button", "name": "Send message", "unique": true, "count": 1, "score": 0.85},
		map[string]any{"kind": "css", "value": "button", "unique": false, "count": 3, "score": 0.1},
	},
}

// repairObs records selector outcomes, Jev picks and hints.
type repairObs struct {
	mu     sync.Mutex
	picks  []string
	entry  []SelectorEntry
	labels []string
	hints  map[string]string
	loads  int
}

func (o *repairObs) ObserveSelector(string, string, int, bool, bool) {}
func (o *repairObs) ObserveJevPick(id, key string, p JevPick) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.picks = append(o.picks, id+"/"+key)
	o.entry = append(o.entry, p.Entry)
	o.labels = append(o.labels, p.Label)
}
func (o *repairObs) LoadJevHint(k string) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.loads++
	v, ok := o.hints[k]
	return v, ok
}
func (o *repairObs) SaveJevHint(k, label string) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.hints[k] = label
}

func forgetJevHint(t *testing.T, key string) {
	t.Helper()
	clear := func() {
		jevHints.Lock()
		delete(jevHints.m, key)
		jevHints.Unlock()
	}
	clear()
	t.Cleanup(clear)
}

func newRepairExecutor(t *testing.T) (*ActionExecutor, *fpPage, *repairObs) {
	t.Helper()
	page := &fpPage{jevFakePage: newJevFakePage(), fp: sendFingerprint}
	ae := newJevExecutor(t, page, true, 0.5)
	ae.SetPackage(&fakePkg{id: "acme", selectors: map[string]*SelectorEntry{
		"send": {Intent: "the chat's Send message button", Candidates: []SelectorCandidate{{CSS: "#gone"}}},
	}})
	obs := &repairObs{hints: map[string]string{}}
	ae.SetSelectorObserver(obs)
	forgetJevHint(t, ae.jevHintKey(StepDef{ID: "s"}))
	return ae, page, obs
}

func TestJevRepairPickOfPackageSelectorIsReported(t *testing.T) {
	jevtest.NewServer(t, jevtest.Fixed(map[string]string{"target": "2"}))
	ae, page, obs := newRepairExecutor(t)
	el, err := ae.resolveElement(StepDef{ID: "s", Type: "click", ConfigKey: "send", Timeout: 0.05})
	if err != nil || el.(*markedElement).node != 20 {
		t.Fatalf("el = %#v err = %v", el, err)
	}
	if page.fpCalls != 1 || len(obs.picks) != 1 || obs.picks[0] != "acme/send" {
		t.Fatalf("fingerprint calls %d, picks %v", page.fpCalls, obs.picks)
	}
	e := obs.entry[0]
	if e.Intent != "the chat's Send message button" || e.VerifiedAt == "" || len(e.Candidates) != 3 {
		t.Fatalf("entry = %+v", e)
	}
	// Unique first, by score: aria (0.85), structural css (0.35), then the non-unique one.
	if e.Candidates[0].Aria == nil || e.Candidates[0].Aria.Name != "Send message" || e.Candidates[1].CSS != "main > button" || e.Candidates[2].CSS != "button" {
		t.Errorf("candidates = %+v", e.Candidates)
	}
	if obs.labels[0] != "Send message" {
		t.Errorf("label = %q", obs.labels[0])
	}
	if got := obs.hints[ae.jevHintKey(StepDef{ID: "s"})]; got != "Send message" {
		t.Errorf("saved hint = %q (hints %v)", got, obs.hints)
	}
	// The marker was still on the element when the fingerprint was read,
	// and stays until the step ends.
	if len(page.marked) != 1 {
		t.Errorf("marked = %v", page.marked)
	}
}

func TestJevRepairNotReportedWithoutPackageSelector(t *testing.T) {
	jevtest.NewServer(t, jevtest.Fixed(map[string]string{"target": "2"}))
	ae, page, obs := newRepairExecutor(t)
	if _, err := ae.resolveElement(StepDef{ID: "s", Type: "click", Selector: "#gone", Intent: "the Send button", Timeout: 0.05}); err != nil {
		t.Fatal(err)
	}
	if page.fpCalls != 0 || len(obs.picks) != 0 {
		t.Errorf("fingerprint calls %d, picks %v", page.fpCalls, obs.picks)
	}
	if len(obs.hints) != 1 {
		t.Errorf("hint not saved: %v", obs.hints)
	}
}

func TestJevHintComesFromStoreAcrossProcesses(t *testing.T) {
	srv := jevtest.NewServer(t, jevtest.Fixed(map[string]string{"target": "2"}))
	ae, _, obs := newRepairExecutor(t)
	key := ae.jevHintKey(StepDef{ID: "s"})
	obs.hints[key] = "Send it" // saved by an earlier process; not in the cache
	if _, err := ae.resolveElement(StepDef{ID: "s", Type: "click", Selector: "#gone", Intent: "the Send button", Timeout: 0.05}); err != nil {
		t.Fatal(err)
	}
	ins := srv.Requests()[0].Questions["target"].Instructions.(map[string]any)
	if h, _ := ins["hint"].(string); !strings.Contains(h, `"Send it"`) {
		t.Errorf("hint = %v, want the stored label", ins["hint"])
	}
	// The pick refreshed the cache: the next lookup does not read the store.
	loads := obs.loads
	if got := ae.jevHint(key); got != "Send message" || obs.loads != loads {
		t.Errorf("cached hint = %q, store loads %d → %d", got, loads, obs.loads)
	}
}

func TestSelectorEntryFromFingerprintDropsSensitiveText(t *testing.T) {
	fp := &recording.Fingerprint{Sensitive: true, Candidates: []recording.Candidate{
		{Kind: "text", Value: "hunter2", Unique: true, Score: 0.65},
		{Kind: "css", Value: "input[name=\"pw\"]", Unique: true, Score: 0.8},
		{Kind: "aria", Unique: true, Score: 0.85}, // no role: dropped
		{Kind: "bogus", Value: "x"},
	}}
	e := SelectorEntryFromFingerprint(fp, "the password", time.Unix(0, 0))
	if len(e.Candidates) != 1 || e.Candidates[0].CSS != `input[name="pw"]` || e.Intent != "the password" {
		t.Errorf("entry = %+v", e)
	}
}
