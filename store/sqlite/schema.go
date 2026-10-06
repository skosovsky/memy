package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/skosovsky/memy"
)

const schemaInteger = "INTEGER"
const schemaText = "TEXT"

// existingSchema separates empty-file bootstrap from validation. Data tables
// cannot be reconstructed from surviving metadata without losing durable state.
func existingSchema(ctx context.Context, tx *sql.Tx) (bool, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type='table' AND name NOT GLOB 'sqlite_*'`)
	if err != nil {
		return false, storageError(err)
	}
	defer func() { _ = rows.Close() }()
	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return false, storageError(err)
		}
		names[name] = true
	}
	if err = rows.Err(); err != nil {
		return false, storageError(err)
	}
	if len(names) == 0 {
		return false, nil
	}
	for _, name := range []string{"memy_schema", "memy_values", "memy_generations"} {
		if !names[name] {
			return false, memy.ErrSchema
		}
	}

	required := map[string][]schemaColumn{
		"memy_schema": {
			{"singleton", schemaInteger, 0, 1},
			{"version", schemaInteger, 1, 0},
			{"cursor_secret", "BLOB", 0, 0},
		},
		"memy_values": {
			{"scope", schemaText, 1, 1},
			{"key", schemaText, 1, 2},
			{"version", schemaInteger, 1, 0},
			{"value", "BLOB", 0, 0},
		},
		"memy_generations": {{"scope", schemaText, 1, 1}, {"generation", schemaInteger, 1, 0}},
	}
	for table, columns := range required {
		if tableErr := validateTableColumns(ctx, tx, table, columns); tableErr != nil {
			return false, tableErr
		}
	}

	return true, nil
}

type schemaColumn struct {
	name, kind       string
	notNull, primary int
}

func validateTableColumns(ctx context.Context, tx *sql.Tx, table string, want []schemaColumn) error {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return errors.Join(memy.ErrSchema, err)
	}
	defer func() { _ = rows.Close() }()
	count := 0
	for rows.Next() {
		var cid int
		var got schemaColumn
		var defaultValue sql.NullString
		if err = rows.Scan(&cid, &got.name, &got.kind, &got.notNull, &defaultValue, &got.primary); err != nil {
			return errors.Join(memy.ErrSchema, err)
		}
		got.kind = strings.ToUpper(got.kind)
		if count >= len(want) || cid != count || got != want[count] || defaultValue.Valid {
			return memy.ErrSchema
		}
		count++
	}
	if err = rows.Err(); err != nil {
		return errors.Join(memy.ErrSchema, err)
	}
	if count != len(want) {
		return memy.ErrSchema
	}
	return nil
}
