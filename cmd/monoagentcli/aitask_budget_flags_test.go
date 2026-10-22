package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/monoes/mono-agent/internal/aitask/budget"
	"github.com/monoes/mono-agent/internal/cli/clicore"
)

// Who may run each `aitask budget` command, and what each flag is. A new
// subcommand or flag fails TestEveryAitaskBudgetFlagIsClassified until it is
// listed, which is the moment to decide whether only the operator may use it.
//
//	operator: an agent, the chat or a workflow is refused (exit 3, operator_only), even with a task's token
//	either:   the operator, or the task's own session for its own task (--task)
var aitaskBudgetPolicy = map[string]struct {
	who   string
	flags map[string]string
	args  []string
}{
	"set":   {"operator", map[string]string{"scope": "routing", "key": "routing", "usd": "routing", "clear": "routing"}, []string{"--scope", "global", "--usd", "9"}},
	"show":  {"either", map[string]string{"task": "filter"}, []string{"--task", "{id}"}},
	"lower": {"either", map[string]string{"task": "routing", "usd": "routing"}, []string{"--task", "{id}", "--usd", "1"}},
}

func TestEveryAitaskBudgetFlagIsClassified(t *testing.T) {
	cfg := sendEnv(t)
	seen := map[string]bool{}
	for _, top := range newAitaskCmd(cfg).Commands() {
		if top.Name() != "budget" {
			continue
		}
		for _, c := range top.Commands() {
			seen[c.Name()] = true
			p, ok := aitaskBudgetPolicy[c.Name()]
			if !ok {
				t.Errorf("aitask budget %s is not classified", c.Name())
				continue
			}
			c.Flags().VisitAll(func(f *pflag.Flag) {
				if f.Name == "help" {
					return
				}
				if _, ok := p.flags[f.Name]; !ok {
					t.Errorf("aitask budget %s --%s is not classified", c.Name(), f.Name)
				}
			})
			for name := range p.flags {
				if c.Flags().Lookup(name) == nil {
					t.Errorf("aitaskBudgetPolicy lists aitask budget %s --%s, which does not exist", c.Name(), name)
				}
			}
		}
	}
	for name := range aitaskBudgetPolicy {
		if !seen[name] {
			t.Errorf("aitaskBudgetPolicy lists aitask budget %s, which does not exist", name)
		}
	}
}

func (r *aitaskRig) budget(t *testing.T, name string, extra ...string) (string, error) {
	t.Helper()
	p := aitaskBudgetPolicy[name]
	args := []string{"budget", name}
	for _, a := range append(append([]string{}, p.args...), extra...) {
		args = append(args, strings.NewReplacer("{id}", r.own, "{other}", r.other).Replace(a))
	}
	return runSendCLI(t, r.cfg, newAitaskCmd(r.cfg), args...)
}

func (r *aitaskRig) caps(t *testing.T) map[string]float64 {
	t.Helper()
	db, err := initDB(r.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := (&budget.Ledger{DB: db.DB}).Caps()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]float64{}
	for _, c := range rows {
		out[c.Scope+":"+c.Key] = c.CapUSD
	}
	return out
}

// Setting a cap is the user's: neither an agent nor a task's own session can,
// and the cap stays as it was.
func TestAitaskBudgetSetIsOperatorOnly(t *testing.T) {
	r := newAitaskRig(t)
	asPlainOperator(t)
	if _, err := r.budget(t, "set"); err != nil {
		t.Fatalf("the operator sets a cap: %v", err)
	}
	if got := r.caps(t)["global:"]; got != 9 {
		t.Fatalf("global cap = %v, want 9", got)
	}
	for _, who := range []func(){func() { asPlainAgent(t) }, func() { asTaskSession(t, r.own, r.ownToken) }} {
		who()
		if _, err := r.budget(t, "set", "--usd", "100"); clicore.ExitCodeFor(err) != 3 || errCode(err) != "operator_only" {
			t.Errorf("an agent set a cap: %v", err)
		}
		if _, err := r.budget(t, "set", "--clear"); errCode(err) != "operator_only" {
			t.Errorf("an agent cleared a cap: %v", err)
		}
	}
	if got := r.caps(t)["global:"]; got != 9 {
		t.Fatalf("an agent changed the cap to %v", got)
	}
}

// The task's session lowers its own conversation's cap and cannot raise it,
// nor touch another task.
func TestAitaskBudgetLowerNeverRaisesForAnAgent(t *testing.T) {
	r := newAitaskRig(t)
	asPlainOperator(t)
	if _, err := r.budget(t, "set"); err != nil { // global 9
		t.Fatal(err)
	}
	asTaskSession(t, r.own, r.ownToken)
	if _, err := r.budget(t, "lower", "--usd", "50"); errCode(err) != budget.CodeCapRaise {
		t.Fatalf("raising above the global 9: %v", err)
	}
	if _, ok := r.caps(t)["conversation:c"]; ok {
		t.Fatal("a refused raise left a cap behind")
	}
	if _, err := r.budget(t, "lower", "--usd", "2"); err != nil {
		t.Fatalf("lowering to 2: %v", err)
	}
	if _, err := r.budget(t, "lower", "--usd", "3"); errCode(err) != budget.CodeCapRaise {
		t.Fatalf("raising its own 2 to 3: %v", err)
	}
	if got := r.caps(t)["conversation:c"]; got != 2 {
		t.Fatalf("conversation cap = %v, want 2", got)
	}
	// Another task's session, and a plain agent, can't lower this task's cap.
	asTaskSession(t, r.other, r.otherToken)
	if _, err := r.budget(t, "lower", "--usd", "1"); errCode(err) != "operator_only" {
		t.Errorf("another task's session lowered this task's cap: %v", err)
	}
	asPlainAgent(t)
	if _, err := r.budget(t, "lower", "--usd", "1"); errCode(err) != "operator_only" {
		t.Errorf("an agent with no token lowered a cap: %v", err)
	}
	// Zero is a valid lowering: no spend.
	asTaskSession(t, r.own, r.ownToken)
	if _, err := r.budget(t, "lower", "--usd", "0"); err != nil {
		t.Fatal(err)
	}
}

func TestAitaskBudgetShow(t *testing.T) {
	r := newAitaskRig(t)
	asPlainOperator(t)
	if _, err := r.budget(t, "set"); err != nil {
		t.Fatal(err)
	}
	out, err := runSendCLI(t, r.cfg, newAitaskCmd(r.cfg), "budget", "show")
	var rep struct{ Caps []struct{ Scope string } }
	if err != nil || json.Unmarshal([]byte(out), &rep) != nil || len(rep.Caps) != 1 || rep.Caps[0].Scope != "global" {
		t.Fatalf("operator show: %v %s", err, out)
	}
	asTaskSession(t, r.own, r.ownToken)
	if _, err := r.budget(t, "show"); err != nil {
		t.Fatalf("a session shows its own task: %v", err)
	}
	if _, err := runSendCLI(t, r.cfg, newAitaskCmd(r.cfg), "budget", "show"); errCode(err) != "operator_only" {
		t.Fatalf("a session showed every cap: %v", err)
	}
	if _, err := r.budget(t, "show", "--task", r.other); errCode(err) != "operator_only" {
		t.Fatalf("a session showed another task: %v", err)
	}
}
