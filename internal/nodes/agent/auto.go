package agent

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"time"

	"github.com/monoes/mono-agent/internal/agentroster"
	"github.com/monoes/mono-agent/internal/monomind"
)

// RuntimeAuto is the runtime value that lets agent.ask pick one.
const RuntimeAuto = "auto"

// autoHelp is what to do when auto has nothing to pick.
const autoHelp = "validate a runtime with `monoagentcli agent validate` (or install one with `monoagentcli agent install <runtime>`), or set \"runtime\" to one from `monoagentcli agent scan --installed`"

// Seams for tests.
var (
	scanRuntimes = monomind.Scan
	listRoster   = agentroster.List
)

// autoChoice is the runtime and model auto picked, and whether the roster
// had validated it.
type autoChoice struct {
	Runtime   string
	Model     string
	Validated bool
}

// pickAuto chooses a runtime for runtime "auto": a ready (recently
// validated) model of the agent roster, the same rule the API's auto model
// falls back to without Jev — the cheapest measured, then the fastest, then
// claude, codex, antigravity and the rest alphabetically. With a model set,
// only roster entries of that model count. With nothing validated, the first
// installed runtime of the scan, on model (or its default).
func pickAuto(ctx context.Context, db *sql.DB, model string) (autoChoice, error) {
	scan, scanErr := scanRuntimes(ctx)
	if db != nil {
		if results, err := listRoster(ctx, db); err == nil && len(results) > 0 {
			if c, ok := readyChoice(agentroster.Build(results, scan, time.Now(), agentroster.DefaultMaxAge), scan, model); ok {
				return c, nil
			}
		}
	}
	if scanErr != nil {
		return autoChoice{}, fmt.Errorf("runtime %q: no validated model, and the runtime scan failed (%v); %s", RuntimeAuto, scanErr, autoHelp)
	}
	if scan != nil {
		for _, a := range scan.Installed() {
			if _, unsupported := a.UnsupportedReason(); unsupported {
				continue
			}
			return autoChoice{Runtime: a.ID, Model: model}, nil
		}
	}
	return autoChoice{}, fmt.Errorf("runtime %q: no validated model and no installed runtime; %s", RuntimeAuto, autoHelp)
}

// readyChoice picks among the roster's ready models.
func readyChoice(roster []agentroster.RuntimeRoster, scan *monomind.ScanResult, model string) (autoChoice, bool) {
	var ready []agentroster.Entry
	for _, rr := range roster {
		if scan != nil && !rr.Installed {
			continue
		}
		if rr.ExecutionSupported != nil && !*rr.ExecutionSupported {
			continue
		}
		for _, m := range rr.Models {
			if m.State != agentroster.StateReady {
				continue
			}
			if model != "" && m.Model != model {
				continue
			}
			ready = append(ready, m)
		}
	}
	if len(ready) == 0 {
		return autoChoice{}, false
	}
	slices.SortStableFunc(ready, func(a, b agentroster.Entry) int {
		return cmp.Or(
			knownFirst(a.CostUSD, a.HasCost, b.CostUSD, b.HasCost),
			knownFirst(float64(a.LatencyMs), a.LatencyMs > 0, float64(b.LatencyMs), b.LatencyMs > 0),
			cmp.Compare(runtimeRank(a.Runtime), runtimeRank(b.Runtime)),
			cmp.Compare(a.Runtime, b.Runtime),
			cmp.Compare(a.Model, b.Model))
	})
	m := ready[0].Model
	if m == agentroster.DefaultModel {
		m = ""
	}
	return autoChoice{Runtime: ready[0].Runtime, Model: m, Validated: true}, true
}

// knownFirst orders by value, with an unmeasured value after every measured one.
func knownFirst(a float64, aKnown bool, b float64, bKnown bool) int {
	switch {
	case aKnown && bKnown:
		return cmp.Compare(a, b)
	case aKnown:
		return -1
	case bKnown:
		return 1
	}
	return 0
}

// runtimeRank mirrors the API's auto order: claude, codex, antigravity, the rest.
func runtimeRank(runtime string) int {
	switch runtime {
	case "claude":
		return 0
	case "codex":
		return 1
	case "antigravity":
		return 2
	}
	return 3
}
