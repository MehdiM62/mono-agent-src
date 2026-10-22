package chat

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/monoes/mono-agent/internal/ai"
	"github.com/monoes/mono-agent/internal/monomind"
)

// execBudget runs one coder turn with extra chat args under operator budget
// settingsBudget and returns the exec line monomind was given ("" when it was
// not run) and the error.
func execBudget(t *testing.T, settingsBudget float64, extra ...string) (line string, code int) {
	t.Helper()
	dbPath := newChatCLITestDB(t)
	bin, argsLog := writeCoderMonomind(t, coderTranscript)
	withCoderCaps(t, monomind.CoderCapabilities...)
	setCoderSettings(t, dbPath, coderSettings{Enabled: true, BudgetUSD: settingsBudget})
	conv, err := openTestChatStore(t, dbPath).CreateConversationMode("default", "agent", "general", "claude", "", "", ai.ModeCoder, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"--conversation", conv.ID, "--turn", "turn-1"}, extra...)
	_, err = runChatCmd(t, dbPath, bin, append(args, "--", "go")...)
	logged, _ := os.ReadFile(argsLog)
	for _, l := range strings.Split(string(logged), "\n") {
		if strings.HasPrefix(l, "agent exec") {
			line = l
		}
	}
	return line, exitCodeFor(err)
}

// A turn's own cap can lower the operator's coder budget, never raise it, and
// 0 is no spend, never unlimited. Dropping the flag handling makes these fail.
func TestCoderTurnBudgetFlagIsHonoured(t *testing.T) {
	for name, tc := range map[string]struct {
		settings float64
		args     []string
		want     string
	}{
		"lower than the operator's":    {3, []string{"--budget-usd", "1.5"}, "--budget-usd 1.5"},
		"higher than the operator's":   {3, []string{"--budget-usd", "10"}, "--budget-usd 3"},
		"operator has no cap set":      {0, []string{"--budget-usd", "2"}, "--budget-usd 2"},
		"no flag keeps the operator's": {3, nil, "--budget-usd 3"},
	} {
		line, code := execBudget(t, tc.settings, tc.args...)
		if code != 0 || !strings.Contains(line+" ", tc.want+" ") {
			t.Errorf("%s: code %d, exec line %q, want %q", name, code, line, tc.want)
		}
	}
}

func TestCoderTurnWithZeroBudgetDoesNotRun(t *testing.T) {
	for _, settings := range []float64{0, 3} {
		line, code := execBudget(t, settings, "--budget-usd", "0")
		if code != 3 || line != "" {
			t.Errorf("operator budget %v: code %d, ran: %q", settings, code, line)
		}
	}
	if line, code := execBudget(t, 3, "--budget-usd", "-1"); code != 3 || line != "" {
		t.Errorf("negative: code %d, ran: %q", code, line)
	}
}

func TestTurnBudgetOnKiloIsRefused(t *testing.T) {
	if _, err := turnBudget(0, ptr(2), "kilo"); err == nil {
		t.Fatal("a capped turn ran on a runtime that can't enforce the cap")
	}
	if b, err := turnBudget(0, nil, "kilo"); err != nil || b != 0 {
		t.Fatalf("an uncapped kilo turn: %v %v", b, err)
	}
}

func ptr(f float64) *float64 { return &f }

// A turn's own cap that is not finite is no spend either: +Inf must not reach
// the runtime as an unlimited budget.
func TestTurnBudgetRefusesNonFiniteCaps(t *testing.T) {
	for _, v := range []float64{math.Inf(1), math.Inf(-1), math.NaN()} {
		if _, err := turnBudget(5, &v, "claude"); err == nil {
			t.Errorf("a turn cap of %v was accepted", v)
		}
	}
}
