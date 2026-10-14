package evalnode

import (
	"context"
	"fmt"
	"strconv"

	"github.com/monoes/mono-agent/internal/nodes/service"
	"github.com/monoes/mono-agent/internal/workflow"
)

// sheetsCall runs the Google Sheets node; tests replace it.
var sheetsCall = func(ctx context.Context, cfg map[string]interface{}) ([]workflow.Item, error) {
	out, err := (&service.GoogleSheetsNode{}).Execute(ctx, workflow.NodeInput{}, cfg)
	if err != nil || len(out) == 0 {
		return nil, err
	}
	return out[0].Items, nil
}

// sheetConfig is the node's config (its credential, already merged in by
// the run) for one Google Sheets call.
func sheetConfig(cfg map[string]interface{}, op, rng string, values []interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(cfg)+4)
	for k, v := range cfg {
		out[k] = v
	}
	for _, k := range metricFields {
		delete(out, k)
	}
	for _, k := range []string{"inputs", "outputs", "source", "table", "metric"} {
		delete(out, k)
	}
	out["operation"], out["range"] = op, rng
	out["sheet_name"] = cfgStr(cfg, "sheet_name", "Sheet1")
	if values != nil {
		out["values"] = values
	} else {
		delete(out, "values")
	}
	return out
}

// columnLetter is the A1 letter of a 0-based column index.
func columnLetter(i int) string {
	s := ""
	for i++; i > 0; i = (i - 1) / 26 {
		s = string(rune('A'+(i-1)%26)) + s
	}
	return s
}

// writeSheetRow writes the outputs into sheet row rowNumber, under the
// header row's columns of their names; missing columns are added to the
// header row first.
func writeSheetRow(ctx context.Context, cfg map[string]interface{}, rowNumber int, list []pair) error {
	if cfgStr(cfg, "spreadsheet_id", "") == "" {
		return fmt.Errorf("%w: no spreadsheet: set \"spreadsheet_id\"", workflow.ErrInvalidConfig)
	}
	if rowNumber < 2 {
		return fmt.Errorf("the dataset row number %d is not a sheet data row", rowNumber)
	}
	head, err := sheetsCall(ctx, sheetConfig(cfg, "read_rows", "1:1", nil))
	if err != nil {
		return fmt.Errorf("read the header row: %w", err)
	}
	var header []string
	if len(head) > 0 {
		for i := 0; ; i++ {
			v, ok := head[0].JSON[columnLetter(i)]
			if !ok {
				break
			}
			header = append(header, fmt.Sprint(v))
		}
	}
	col := map[string]int{}
	for i, h := range header {
		if _, seen := col[h]; !seen {
			col[h] = i
		}
	}
	grown := false
	for _, p := range list {
		if _, ok := col[p.name]; !ok {
			col[p.name] = len(header)
			header = append(header, p.name)
			grown = true
		}
	}
	if grown {
		row := make([]interface{}, len(header))
		for i, h := range header {
			row[i] = h
		}
		if _, err := sheetsCall(ctx, sheetConfig(cfg, "update_rows", "A1", []interface{}{row})); err != nil {
			return fmt.Errorf("write the header row: %w", err)
		}
	}
	for _, p := range list {
		cell := columnLetter(col[p.name]) + strconv.Itoa(rowNumber)
		if _, err := sheetsCall(ctx, sheetConfig(cfg, "update_rows", cell, []interface{}{[]interface{}{cellValue(p.value)}})); err != nil {
			return fmt.Errorf("write %s: %w", cell, err)
		}
	}
	return nil
}

// cellValue is a value as a sheet cell takes it: text, a number or a
// boolean as it is, anything else as JSON text.
func cellValue(v interface{}) interface{} {
	switch v.(type) {
	case nil:
		return ""
	case string, float64, int, int64, bool:
		return v
	}
	return text(v)
}
