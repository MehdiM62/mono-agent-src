package control

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/monoes/mono-agent/internal/datatable"
	"github.com/monoes/mono-agent/internal/workflow"
)

// tableJSON is a table as the node outputs it.
func tableJSON(t datatable.Table) map[string]interface{} {
	cols := make([]interface{}, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = map[string]interface{}{"name": c.Name, "type": c.Type, "index": i}
	}
	return map[string]interface{}{"id": t.ID, "name": t.Name, "columns": cols, "createdAt": t.CreatedAt, "updatedAt": t.UpdatedAt}
}

// tableOps runs a table operation once per item.
func (r *dataTableRun) tableOps(ctx context.Context, op string, items []workflow.Item) ([]workflow.Item, error) {
	var out []workflow.Item
	for i, item := range items {
		ic, err := r.resolve(item)
		if err != nil {
			return nil, fmt.Errorf("item %d: %w", i, err)
		}
		var res []map[string]interface{}
		switch op {
		case "create":
			res, err = r.createTable(ctx, ic.table)
		case "list":
			res, err = r.listTables(ctx)
		case "update", "rename":
			var t *datatable.Table
			if t, err = r.table(ctx, ic.table); err == nil {
				name := configStr(r.config, "new_name", "")
				var renamed datatable.Table
				if renamed, err = r.store.RenameTable(ctx, *t, name); err == nil {
					*t = renamed
					res = []map[string]interface{}{{"success": true, "name": name}}
				}
			}
		case "clear":
			var t *datatable.Table
			if t, err = r.table(ctx, ic.table); err == nil {
				var n int64
				if n, err = r.store.ClearTable(ctx, *t); err == nil {
					res = []map[string]interface{}{{"success": true, "deletedCount": n}}
				}
			}
		case "delete":
			var t *datatable.Table
			if t, err = r.table(ctx, ic.table); err == nil {
				if err = r.store.DeleteTable(ctx, *t); err == nil {
					delete(r.tables, ic.table)
					res = []map[string]interface{}{{"success": true, "deletedTableId": t.ID}}
				}
			}
		default:
			return nil, fmt.Errorf("%w: table operation %q (use create, list, update, clear or delete)", workflow.ErrInvalidConfig, op)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, rowItems(res)...)
	}
	return out, nil
}

func (r *dataTableRun) createTable(ctx context.Context, name string) ([]map[string]interface{}, error) {
	if configBool(r.config, "reuse_existing", true) {
		if t, err := r.store.FindTable(ctx, name); err == nil && t.Name == name {
			return []map[string]interface{}{tableJSON(t)}, nil
		} else if err != nil && !errors.Is(err, datatable.ErrNotFound) {
			return nil, err
		}
	}
	var cols []datatable.Column
	list, _ := jsonValue(r.config["columns"]).([]interface{})
	for i, c := range list {
		m, ok := c.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%w: columns[%d] must be {name, type}", workflow.ErrInvalidConfig, i)
		}
		name, _ := m["name"].(string)
		typ, _ := m["type"].(string)
		cols = append(cols, datatable.Column{Name: strings.TrimSpace(name), Type: typ})
	}
	t, err := r.store.CreateTable(ctx, name, cols)
	if err != nil {
		return nil, err
	}
	return []map[string]interface{}{tableJSON(t)}, nil
}

func (r *dataTableRun) listTables(ctx context.Context) ([]map[string]interface{}, error) {
	tables, err := r.store.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	if f := strings.ToLower(configStr(r.config, "filter_name", "")); f != "" {
		kept := tables[:0]
		for _, t := range tables {
			if strings.Contains(strings.ToLower(t.Name), f) {
				kept = append(kept, t)
			}
		}
		tables = kept
	}
	field := configStr(r.config, "sort_field", "name")
	desc := strings.EqualFold(configStr(r.config, "sort_direction", "asc"), "desc")
	key := func(t datatable.Table) string {
		switch field {
		case "createdAt":
			return t.CreatedAt
		case "updatedAt":
			return t.UpdatedAt
		}
		return strings.ToLower(t.Name)
	}
	sort.SliceStable(tables, func(i, j int) bool {
		if desc {
			return key(tables[i]) > key(tables[j])
		}
		return key(tables[i]) < key(tables[j])
	})
	if !configBool(r.config, "return_all", true) {
		limit := int(configNumber(r.config, "limit"))
		if limit <= 0 {
			limit = dataTableDefaultLimit
		}
		if len(tables) > limit {
			tables = tables[:limit]
		}
	}
	out := make([]map[string]interface{}, len(tables))
	for i, t := range tables {
		out[i] = tableJSON(t)
	}
	return out, nil
}
