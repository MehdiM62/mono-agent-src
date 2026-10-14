package evaluation

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/monoes/mono-agent/internal/datatable"
	"github.com/monoes/mono-agent/internal/nodes/service"
	"github.com/monoes/mono-agent/internal/workflow"
)

// Dataset sources.
const (
	SourceDataTable    = "dataTable"
	SourceGoogleSheets = "googleSheets"
)

// Dataset is where a run's rows come from, as its trigger.evaluation node
// names it (workflow.EvaluationTriggerSchema).
type Dataset struct {
	Source        string
	Table         string
	CredentialID  string
	SpreadsheetID string
	SheetName     string
	// Limit is the most rows to run; 0 runs them all.
	Limit  int
	Filter datatable.Filter
}

// Row is one dataset row: the trigger item it becomes, its id and number
// (a data table's row id and position, or a sheet's row number) and the
// id of the data table it is in.
type Row struct {
	JSON   map[string]interface{}
	ID     string
	Number int
	Table  string
}

// ParseDataset reads a trigger.evaluation config.
func ParseDataset(cfg map[string]interface{}) (Dataset, error) {
	d := Dataset{
		Source:        str(cfg, "source", SourceDataTable),
		Table:         str(cfg, "table", ""),
		CredentialID:  str(cfg, "credential_id", ""),
		SpreadsheetID: str(cfg, "spreadsheet_id", ""),
		SheetName:     str(cfg, "sheet_name", "Sheet1"),
	}
	if b, _ := cfg["limit_rows"].(bool); b {
		d.Limit = int(num(cfg["max_rows"], 10))
		if d.Limit < 1 {
			return d, fmt.Errorf("max_rows must be at least 1")
		}
	}
	d.Filter.All = str(cfg, "match", "any") == "all"
	list := cfg["filters"]
	if s, ok := list.(string); ok && strings.TrimSpace(s) != "" {
		if err := json.Unmarshal([]byte(s), &list); err != nil {
			return d, fmt.Errorf("filters: %v", err)
		}
	}
	items, _ := list.([]interface{})
	for i, f := range items {
		m, ok := f.(map[string]interface{})
		if !ok {
			return d, fmt.Errorf("filters[%d] must be {column, condition, value}", i)
		}
		c := datatable.Condition{Column: strings.TrimSpace(fmt.Sprint(m["column"])), Value: m["value"]}
		c.Op, _ = m["condition"].(string)
		if m["column"] == nil || c.Column == "" {
			return d, fmt.Errorf("filters[%d] has no column", i)
		}
		d.Filter.Conditions = append(d.Filter.Conditions, c)
	}
	switch d.Source {
	case SourceDataTable:
		if d.Table == "" {
			return d, fmt.Errorf("no data table: set the trigger's \"table\" (or pass --dataset)")
		}
	case SourceGoogleSheets:
		if d.SpreadsheetID == "" {
			return d, fmt.Errorf("no spreadsheet: set the trigger's \"spreadsheet_id\"")
		}
	default:
		return d, fmt.Errorf("unknown source %q (use dataTable or googleSheets)", d.Source)
	}
	return d, nil
}

// Describe is the dataset as a run records it.
func (d Dataset) Describe() map[string]interface{} {
	m := map[string]interface{}{"source": d.Source}
	if d.Source == SourceGoogleSheets {
		m["spreadsheet_id"], m["sheet_name"] = d.SpreadsheetID, d.SheetName
	} else {
		m["table"] = d.Table
	}
	if d.Limit > 0 {
		m["max_rows"] = d.Limit
	}
	if len(d.Filter.Conditions) > 0 {
		m["filters"] = d.Filter.Conditions
	}
	return m
}

// CredentialResolver merges a node's credential_id into its config, as a
// run does (workflow.WorkflowEngine.ResolveCredentials).
type CredentialResolver func(ctx context.Context, nodeType string, cfg map[string]interface{}) map[string]interface{}

// readSheet runs the Google Sheets node's read_rows; tests replace it.
var readSheet = func(ctx context.Context, cfg map[string]interface{}) ([]workflow.Item, error) {
	out, err := (&service.GoogleSheetsNode{}).Execute(ctx, workflow.NodeInput{}, cfg)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return out[0].Items, nil
}

// LoadRows reads the dataset's rows, filtered and limited, in order.
func (d Dataset) LoadRows(ctx context.Context, db *sql.DB, profile string, resolve CredentialResolver) ([]Row, error) {
	if d.Source == SourceGoogleSheets {
		return d.sheetRows(ctx, resolve)
	}
	store := datatable.New(db, profile)
	t, err := store.FindTable(ctx, d.Table)
	if err != nil {
		return nil, err
	}
	found, err := store.Select(ctx, t, datatable.Query{Filter: d.Filter, Limit: d.Limit})
	if err != nil {
		return nil, err
	}
	rows := make([]Row, len(found))
	for i, r := range found {
		id := fmt.Sprint(r["id"])
		r["row_id"], r["row_number"] = r["id"], i
		rows[i] = Row{JSON: r, ID: id, Number: i, Table: t.ID}
	}
	return rows, nil
}

// sheetRows reads the sheet (its first row names the columns). A row's
// number is its row in the sheet, as n8n's row_number.
func (d Dataset) sheetRows(ctx context.Context, resolve CredentialResolver) ([]Row, error) {
	cfg := map[string]interface{}{"credential_id": d.CredentialID, "operation": "read_rows",
		"spreadsheet_id": d.SpreadsheetID, "sheet_name": d.SheetName, "use_header_row": true}
	if resolve != nil {
		cfg = resolve(ctx, "service.google_sheets", cfg)
	}
	items, err := readSheet(ctx, cfg)
	if err != nil {
		return nil, err
	}
	var rows []Row
	for _, it := range items {
		m := make(map[string]interface{}, len(it.JSON))
		for k, v := range it.JSON {
			m[k] = v
		}
		n := int(num(m["_row_index"], 0))
		delete(m, "_row_index")
		delete(m, "_row_range")
		if !d.sheetMatch(m) {
			continue
		}
		m["row_number"] = n
		rows = append(rows, Row{JSON: m, ID: strconv.Itoa(n), Number: n})
		if d.Limit > 0 && len(rows) == d.Limit {
			break
		}
	}
	return rows, nil
}

// sheetMatch compares a sheet row with the filters (as text, eq only).
func (d Dataset) sheetMatch(row map[string]interface{}) bool {
	if len(d.Filter.Conditions) == 0 {
		return true
	}
	for _, c := range d.Filter.Conditions {
		ok := fmt.Sprint(valueOr(row[c.Column])) == fmt.Sprint(valueOr(c.Value))
		if ok != d.Filter.All {
			return ok
		}
	}
	return d.Filter.All
}

func valueOr(v interface{}) interface{} {
	if v == nil {
		return ""
	}
	return v
}

func str(cfg map[string]interface{}, key, def string) string {
	if s, ok := cfg[key].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return def
}

func num(v interface{}, def float64) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(n), 64); err == nil {
			return f
		}
	}
	return def
}
