// Package kv implements the transaction-local CAS semantics shared by stores.
package kv

import (
	"bytes"
	"context"
	"github.com/skosovsky/memy/internal/workcost"
	"strings"

	"github.com/skosovsky/memy"
)

// Bucket owns a detached snapshot for one transaction.
type Bucket struct {
	Values      map[string]memy.Value
	index       *Index
	destination *Index
	Binding     Binding
	pending     map[string]memy.Value
	ctx         context.Context
	writable    bool
	closed      bool
}

// New creates an addressed transaction overlay. The owner must exclude writes
// to values through the callback; no complete snapshot or payload copy is made.
func New(ctx context.Context, values map[string]memy.Value, index *Index, binding Binding, writable bool) *Bucket {
	owned := *index
	return &Bucket{Values: values, index: &owned, destination: index, Binding: binding, pending: make(map[string]memy.Value), ctx: ctx, writable: writable, closed: false}
}

// Commit applies only touched keys after the owner's successful transaction.
// The caller must hold the same exclusion boundary used for the callback.
func (b *Bucket) Commit() {
	for key, value := range b.pending {
		b.Values[key] = value
	}
	*b.destination = *b.index
}

func (b *Bucket) value(key string) memy.Value {
	if value, exists := b.pending[key]; exists {
		return value
	}
	return b.Values[key]
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
	value := b.value(key)
	workcost.Copy(len(value.Data))
	value.Data = bytes.Clone(value.Data)
	return value, nil
}

// Scan visits only the live ordered prefix range, including transaction writes.
func (b *Bucket) Scan(options memy.ScanOptions) (memy.ScanPage, error) {
	if err := b.check("scan"); err != nil {
		return memy.ScanPage{}, err
	}
	after, err := b.Binding.Resume(options)
	if err != nil {
		return memy.ScanPage{}, err
	}
	keys := b.index.Keys(options.Prefix, after, options.Limit+1)
	page := memy.ScanPage{Entries: make([]memy.Entry, 0, min(len(keys), options.Limit)), Complete: true}
	for _, key := range keys {
		if err := b.ctx.Err(); err != nil {
			return memy.ScanPage{}, err
		}
		value := b.value(key)
		if len(page.Entries) == options.Limit || len(key) > options.MaxBytes-page.Bytes || len(value.Data) > options.MaxBytes-page.Bytes-len(key) {
			if len(page.Entries) == 0 {
				return memy.ScanPage{}, memy.ErrBudget
			}
			page.Complete = false
			page.Cursor = b.Binding.Next(options, page.Entries[len(page.Entries)-1].Key)
			break
		}
		workcost.Copy(len(value.Data))
		value.Data = bytes.Clone(value.Data)
		page.Entries = append(page.Entries, memy.Entry{Key: key, Value: value})
		page.Bytes += len(key) + len(value.Data)
	}
	return page, nil
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
	old := b.value(key)
	if old.Version != expected {
		return 0, memy.ErrConflict
	}
	if expected >= memy.MaxVersion || b.Binding.Generation >= memy.MaxVersion {
		return 0, memy.ErrConflict
	}
	next := expected + 1
	b.pending[key] = memy.Value{Version: next, Data: data}
	if data == nil {
		b.index.Remove(key)
	} else {
		b.index.Add(key)
	}
	b.Binding.Generation++
	return next, nil
}
