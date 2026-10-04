// Package kv implements the transaction-local CAS semantics shared by stores.
package kv

import (
	"bytes"
	"context"
	"slices"
	"strings"

	"github.com/skosovsky/memy"
)

// Bucket owns a detached snapshot for one transaction.
type Bucket struct {
	Values   map[string]memy.Value
	ctx      context.Context
	writable bool
	closed   bool
}

// New creates a transaction snapshot. Values must be privately owned.
func New(ctx context.Context, values map[string]memy.Value, writable bool) *Bucket {
	return &Bucket{Values: values, ctx: ctx, writable: writable, closed: false}
}

// Seal invalidates all operations on an escaped transaction.
func (b *Bucket) Seal() { b.closed = true }

func (b *Bucket) check(key string) error {
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

// Get returns a detached value or a zero-version absent value.
func (b *Bucket) Get(key string) (memy.Value, error) {
	if err := b.check(key); err != nil {
		return memy.Value{}, err
	}
	value := b.Values[key]
	value.Data = bytes.Clone(value.Data)
	return value, nil
}

// List returns live entries in deterministic key order.
func (b *Bucket) List(prefix string) ([]memy.Entry, error) {
	if err := b.check("list"); err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(b.Values))
	for key, value := range b.Values {
		if value.Data != nil && strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	entries := make([]memy.Entry, 0, len(keys))
	for _, key := range keys {
		value := b.Values[key]
		value.Data = bytes.Clone(value.Data)
		entries = append(entries, memy.Entry{Key: key, Value: value})
	}
	return entries, nil
}

// Put conditionally writes a live value, incrementing its version.
func (b *Bucket) Put(key string, expected memy.Version, data []byte) (memy.Version, error) {
	if len(data) == 0 {
		return 0, memy.ErrInvalid
	}
	return b.write(key, expected, bytes.Clone(data))
}

// Delete leaves a version tombstone. Deleting an absent key is a real CAS write.
func (b *Bucket) Delete(key string, expected memy.Version) (memy.Version, error) {
	return b.write(key, expected, nil)
}

func (b *Bucket) write(key string, expected memy.Version, data []byte) (memy.Version, error) {
	if err := b.check(key); err != nil {
		return 0, err
	}
	if !b.writable {
		return 0, memy.ErrUnsupported
	}
	old := b.Values[key]
	if old.Version != expected {
		return 0, memy.ErrConflict
	}
	if expected >= memy.MaxVersion {
		return 0, memy.ErrConflict
	}
	next := expected + 1
	b.Values[key] = memy.Value{Version: next, Data: data}
	return next, nil
}

// Clone detaches all bytes from a snapshot.
func Clone(values map[string]memy.Value) map[string]memy.Value {
	result := make(map[string]memy.Value, len(values))
	for key, value := range values {
		value.Data = bytes.Clone(value.Data)
		result[key] = value
	}
	return result
}
