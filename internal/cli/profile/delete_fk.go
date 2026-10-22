package profile

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// fkViolation is one row PRAGMA foreign_key_check reports: a row of Table
// whose foreign key FKID points at a parent row that does not exist.
type fkViolation struct {
	Table string
	RowID sql.NullInt64
	FKID  int
}

func foreignKeyViolations(ctx context.Context, q scopeQuerier) (map[fkViolation]bool, error) {
	rows, err := q.QueryContext(ctx, `PRAGMA foreign_key_check`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[fkViolation]bool{}
	for rows.Next() {
		var v fkViolation
		var parent string
		if err := rows.Scan(&v.Table, &v.RowID, &parent, &v.FKID); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

// repairDanglingReferences handles the rows that point at what the delete just
// removed through a foreign key that does not cascade (a post of another
// profile naming one of this profile's people, a node target with no
// execution): the reference is cleared when the column allows it, else the
// row goes. Violations that were there before the delete are not touched.
func repairDanglingReferences(ctx context.Context, tx *sql.Tx, before map[fkViolation]bool) error {
	for pass := 0; pass < 8; pass++ {
		now, err := foreignKeyViolations(ctx, tx)
		if err != nil {
			return err
		}
		fresh := 0
		for v := range now {
			if before[v] {
				continue
			}
			fresh++
			if !v.RowID.Valid {
				return fmt.Errorf("%s has a row left pointing at the deleted profile, and no rowid to repair it by", v.Table)
			}
			if err := repairOne(ctx, tx, v); err != nil {
				return err
			}
		}
		if fresh == 0 {
			return nil
		}
	}
	return fmt.Errorf("foreign key violations remain after deleting the profile")
}

func repairOne(ctx context.Context, tx *sql.Tx, v fkViolation) error {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf(`SELECT f."from", COALESCE(t."notnull", 1) FROM pragma_foreign_key_list('%s') f LEFT JOIN pragma_table_info('%s') t ON t.name = f."from" WHERE f.id = ?`, v.Table, v.Table), v.FKID)
	if err != nil {
		return err
	}
	var sets []string
	nullable := true
	for rows.Next() {
		var c string
		var notNull int
		if err := rows.Scan(&c, &notNull); err != nil {
			rows.Close()
			return err
		}
		sets = append(sets, fmt.Sprintf(`"%s" = NULL`, c))
		nullable = nullable && notNull == 0
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if len(sets) == 0 {
		return fmt.Errorf("%s: no foreign key %d to repair", v.Table, v.FKID)
	}
	if !nullable {
		_, err := tx.ExecContext(ctx, fmt.Sprintf(`DELETE FROM "%s" WHERE rowid = ?`, v.Table), v.RowID.Int64)
		return err
	}
	_, err = tx.ExecContext(ctx, fmt.Sprintf(`UPDATE "%s" SET %s WHERE rowid = ?`, v.Table, strings.Join(sets, ", ")), v.RowID.Int64)
	return err
}
