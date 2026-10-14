package service

import (
	"context"

	"github.com/monoes/mono-agent/internal/workflow"
)

// sheetsCursor is what a since_last_run read_rows remembers about its
// previous run: how many rows the range held, header row included.
type sheetsCursor struct {
	Rows int `json:"rows"`
}

// sheetsRowsSinceLastRun keeps the items of rows past the ones the node's
// previous read of this spreadsheet and range saw, then stores the row count
// for the next run. The first run keeps every row. A range that now holds
// fewer rows than before was cleared or had rows deleted, so the row numbers
// no longer line up: that run starts over and keeps every row, as a first
// run does. The cursor is per node, spreadsheet and range (sheet name
// included), so changing any of them starts over.
func sheetsRowsSinceLastRun(ctx context.Context, input workflow.NodeInput, spreadsheetID, rangeStr string, rows int, items []workflow.Item) ([]workflow.Item, error) {
	sd, err := workflow.StaticDataFor(ctx, input, workflow.StateScopeNode)
	if err != nil {
		return nil, err
	}
	key := "sheets_cursor:" + spreadsheetID + "\x00" + rangeStr
	var prev sheetsCursor
	found, err := sd.Get(ctx, key, &prev)
	if err != nil {
		return nil, err
	}
	fresh := items
	if found && rows >= prev.Rows {
		fresh = make([]workflow.Item, 0, len(items))
		for _, it := range items {
			if n, _ := it.JSON["_row_index"].(int); n > prev.Rows {
				fresh = append(fresh, it)
			}
		}
	}
	if err := sd.Set(ctx, key, sheetsCursor{Rows: rows}); err != nil {
		return nil, err
	}
	return fresh, nil
}
