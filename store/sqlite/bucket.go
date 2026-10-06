package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/skosovsky/memy/internal/workcost"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/kv"
)

const maxPrefixByte = 255

// sqlBucket performs addressed SQL inside the owning transaction. It never
// materializes unrelated scope rows and never outlives its synchronous callback.
type sqlBucket struct {
	ctx      context.Context
	tx       *sql.Tx
	scope    string
	writable bool
	closed   bool
	binding  kv.Binding
}

func (b *sqlBucket) Seal() { b.closed = true }

func (b *sqlBucket) check(key string) error {
	if b.closed {
		return memy.ErrClosed
	}
	if err := b.ctx.Err(); err != nil {
		return err
	}
	if key == "" || len(key) > 4096 || strings.ContainsRune(key, '\x00') {
		return memy.ErrInvalid
	}
	return nil
}

func validValue(v memy.Value) bool {
	return v.Version > 0 && v.Version <= memy.MaxVersion && (v.Data == nil || len(v.Data) > 0)
}

func (b *sqlBucket) Get(key string) (memy.Value, error) {
	if err := b.check(key); err != nil {
		return memy.Value{}, err
	}
	var v memy.Value
	err := b.tx.QueryRowContext(b.ctx, `SELECT version,value FROM memy_values WHERE scope=? AND key=?`, b.scope, key).
		Scan(&v.Version, &v.Data)
	if errors.Is(err, sql.ErrNoRows) {
		return memy.Value{}, nil
	}
	if err != nil {
		return memy.Value{}, storageError(err)
	}
	workcost.Value(len(v.Data))
	if !validValue(v) {
		return memy.Value{}, memy.ErrSchema
	}
	return v, nil
}

// prefixEnd is the exclusive lexicographic upper bound, when one exists.
func prefixEnd(prefix string) string {
	end := []byte(prefix)
	for i := len(end) - 1; i >= 0; i-- {
		if end[i] != maxPrefixByte {
			end[i]++
			return string(end[:i+1])
		}
	}
	return ""
}

func (b *sqlBucket) Scan(options memy.ScanOptions) (memy.ScanPage, error) {
	if err := b.check("scan"); err != nil {
		return memy.ScanPage{}, err
	}
	after, err := b.binding.Resume(options)
	if err != nil {
		return memy.ScanPage{}, err
	}
	query := `SELECT key,version,length(value) FROM memy_values WHERE scope=? AND key>=? AND key>? AND value IS NOT NULL`
	args := []any{b.scope, options.Prefix, after}
	if end := prefixEnd(options.Prefix); end != "" {
		query += ` AND key<?`
		args = append(args, end)
	}
	query += ` ORDER BY key LIMIT ?`
	args = append(args, options.Limit+1)
	rows, err := b.tx.QueryContext(b.ctx, query, args...)
	if err != nil {
		return memy.ScanPage{}, storageError(err)
	}
	defer func() { _ = rows.Close() }()
	page, scanErr := b.scanMetadata(rows, options)
	if scanErr != nil {
		return memy.ScanPage{}, scanErr
	}

	for i := range page.Entries {
		value, err := b.Get(page.Entries[i].Key)
		if err != nil {
			return memy.ScanPage{}, err
		}
		if value.Data == nil || value.Version != page.Entries[i].Value.Version {
			return memy.ScanPage{}, memy.ErrSchema
		}
		page.Entries[i].Value = value
	}
	return page, nil
}

func (b *sqlBucket) Put(key string, expected memy.Version, data []byte) (memy.Version, error) {
	if len(data) == 0 {
		return 0, memy.ErrInvalid
	}
	return b.write(key, expected, data)
}

func (b *sqlBucket) Delete(key string, expected memy.Version) (memy.Version, error) {
	return b.write(key, expected, nil)
}

func (b *sqlBucket) write(key string, expected memy.Version, data []byte) (memy.Version, error) {
	if err := b.check(key); err != nil {
		return 0, err
	}
	if !b.writable {
		return 0, memy.ErrUnsupported
	}
	if expected >= memy.MaxVersion || b.binding.Generation >= memy.MaxVersion {
		return 0, memy.ErrConflict
	}
	next := expected + 1
	var result sql.Result
	var err error
	if expected == 0 {
		result, err = b.tx.ExecContext(
			b.ctx,
			`INSERT OR IGNORE INTO memy_values(scope,key,version,value) VALUES(?,?,?,?)`,
			b.scope,
			key,
			next,
			data,
		)
	} else {
		result, err = b.tx.ExecContext(
			b.ctx,
			`UPDATE memy_values SET version=?,value=? WHERE scope=? AND key=? AND version=?`,
			next,
			data,
			b.scope,
			key,
			expected,
		)
	}
	if err != nil {
		return 0, storageError(err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return 0, storageError(err)
	}
	if changed != 1 {
		return 0, memy.ErrConflict
	}
	b.binding.Generation++
	return next, nil
}

func (b *sqlBucket) scanMetadata(rows *sql.Rows, options memy.ScanOptions) (memy.ScanPage, error) {
	page := memy.ScanPage{Entries: make([]memy.Entry, 0), Complete: true, Cursor: "", Bytes: 0}
	for rows.Next() {
		workcost.Metadata()
		var entry memy.Entry
		var size int64
		if err := rows.Scan(&entry.Key, &entry.Value.Version, &size); err != nil {
			return memy.ScanPage{}, storageError(err)
		}
		if entry.Value.Version == 0 || entry.Value.Version > memy.MaxVersion || size <= 0 {
			return memy.ScanPage{}, memy.ErrSchema
		}
		if len(page.Entries) == options.Limit || len(entry.Key) > options.MaxBytes-page.Bytes ||
			size > int64(options.MaxBytes-page.Bytes-len(entry.Key)) {
			if len(page.Entries) == 0 {
				return memy.ScanPage{}, memy.ErrBudget
			}
			page.Complete = false
			page.Cursor = b.binding.Next(options, page.Entries[len(page.Entries)-1].Key)
			break
		}
		page.Entries = append(page.Entries, entry)
		page.Bytes += len(entry.Key) + int(size)
	}
	if err := rows.Err(); err != nil {
		return memy.ScanPage{}, storageError(err)
	}
	if err := rows.Close(); err != nil {
		return memy.ScanPage{}, storageError(err)
	}
	return page, nil
}
