package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/monoes/mono-agent/internal/monomind"
	"github.com/monoes/mono-agent/internal/orgdesign"
)

// Org runs in the history: one row per run of every org of the profile, read
// from monomind's report (`org report <name> --all`). Reading it starts a
// monomind process, so a report is kept for orgHistoryTTL per org and root.

const orgHistoryTTL = 30 * time.Second

// Stand-ins for tests.
var (
	orgNamesFn  = orgdesign.ListOrgNames
	orgReportFn = monomind.OrgReport
	orgLiveFn   = monomind.OrgRunLive
	orgNow      = time.Now
)

type orgReportEntry struct {
	at   time.Time
	rows []HistoryRow
}

var (
	orgReportMu    sync.Mutex
	orgReportCache = map[string]orgReportEntry{}
)

// orgHistoryRows is the org run rows that pass f. One org that fails to
// report leaves the others in and comes back as a warning.
func orgHistoryRows(ctx context.Context, root string, f historyFilter) ([]HistoryRow, []string) {
	if root == "" {
		return nil, nil
	}
	names, err := orgNamesFn(root)
	if err != nil {
		return nil, []string{fmt.Sprintf("orgs: %v", err)}
	}
	type result struct {
		rows []HistoryRow
		warn string
	}
	results := make([]result, len(names))
	sem := make(chan struct{}, 4)
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			rows, err := orgRunRows(ctx, root, name)
			if err != nil {
				results[i].warn = fmt.Sprintf("org %s: %v", name, err)
				return
			}
			results[i].rows = rows
		}(i, name)
	}
	wg.Wait()
	var out []HistoryRow
	var warns []string
	q := strings.ToLower(f.query)
	for _, res := range results {
		if res.warn != "" {
			warns = append(warns, res.warn)
		}
		for _, r := range res.rows {
			switch {
			case f.status != "" && r.Status != f.status:
			case f.cursor != "" && r.sortKey() >= f.cursor:
			case q != "" && !strings.Contains(strings.ToLower(r.Title+" "+r.OrgName), q):
			default:
				out = append(out, r)
			}
		}
	}
	return out, warns
}

// orgRunRows is one org's runs, from the cache when it is fresh. The newest
// run that has no outcome is running while the org's process is live.
func orgRunRows(ctx context.Context, root, name string) ([]HistoryRow, error) {
	key := root + "\x00" + name
	orgReportMu.Lock()
	e, ok := orgReportCache[key]
	orgReportMu.Unlock()
	if !ok || orgNow().Sub(e.at) >= orgHistoryTTL {
		raw, err := orgReportFn(ctx, root, name, true, "")
		if err != nil {
			return nil, err
		}
		rows, err := parseOrgRuns(name, raw)
		if err != nil {
			return nil, err
		}
		e = orgReportEntry{at: orgNow(), rows: rows}
		orgReportMu.Lock()
		orgReportCache[key] = e
		orgReportMu.Unlock()
	}
	rows := append([]HistoryRow(nil), e.rows...)
	if len(rows) > 0 && rows[0].Status == histIdle && orgLiveFn(root, name) {
		rows[0].Status = histRunning
	}
	return rows, nil
}

var (
	orgStartKeys = []string{"started_at", "startedAt", "start", "started", "ts"}
	orgEndKeys   = []string{"ended_at", "endedAt", "finished_at", "finishedAt", "closed_at", "closedAt", "completed_at", "updated_at"}
	orgCostKeys  = []string{"cost_usd", "total_cost_usd", "costUsd", "cost"}
)

// parseOrgRuns reads `org report --all`: {"items":[{run, outcome:{status},
// <start/end time>, <cost>}]} or a bare list, newest first by updatedAt. The
// fields besides run are read tolerantly: monomind's report has no schema
// pinned here, and a run with no time sorts last.
func parseOrgRuns(name string, raw json.RawMessage) ([]HistoryRow, error) {
	var items []map[string]any
	var env struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal(raw, &env); err == nil {
		items = env.Items
	} else if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("unreadable report: %w", err)
	}
	rows := make([]HistoryRow, 0, len(items))
	for _, it := range items {
		run, _ := it["run"].(string)
		if run == "" {
			continue
		}
		start := firstString(it, orgStartKeys)
		r := HistoryRow{
			ID: "org:" + name + "#" + run, Kind: histKindOrg, OrgName: name, RunID: run,
			Status: histIdle, UpdatedAt: firstString(it, orgEndKeys),
		}
		if r.UpdatedAt == "" {
			r.UpdatedAt = start
		}
		r.Title = name
		if start != "" {
			r.Title = name + " · " + start
		}
		if oc, ok := it["outcome"].(map[string]any); ok {
			switch oc["status"] {
			case "achieved", "partial":
				r.Status = histDone
			case "failed":
				r.Status = histError
			}
		}
		for _, k := range orgCostKeys {
			if c, ok := it[k].(float64); ok {
				r.CostUSD = &c
				break
			}
		}
		rows = append(rows, r)
	}
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].sortKey() > rows[j].sortKey() })
	return rows, nil
}

func firstString(m map[string]any, keys []string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
