package conformance

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/skosovsky/memy"
)

const cursorBoundaryKeyBytes = 4096
const cursorBoundaryMaxBytes = 10000
const cursorEncodedMaximum = 5563
const cursorIdentifierMaximum = 1024

func scanCursorBoundaries(t *testing.T, store memy.Store) {
	t.Helper()
	// Arrange: worst-case JSON escaping in scope, raw non-UTF8 binary keys/plan.
	scope := memy.Scope{
		Tenant:    strings.Repeat("<", cursorIdentifierMaximum),
		Namespace: strings.Repeat("&", cursorIdentifierMaximum),
		Subject:   strings.Repeat("\"", cursorIdentifierMaximum),
	}
	prefix := strings.Repeat("\xff", cursorBoundaryKeyBytes-1)
	after := prefix + "\x01"
	keys := []string{prefix + "\x02", prefix + "\x03", prefix + "\xff"}
	must(t, store.Update(t.Context(), scope, func(bucket memy.Bucket) error {
		for _, key := range keys {
			if _, err := bucket.Put(key, 0, []byte("value")); err != nil {
				return err
			}
		}
		return nil
	}))
	options := memy.ScanOptions{
		Prefix:   prefix,
		After:    after,
		Plan:     strings.Repeat("\x00", cursorIdentifierMaximum),
		Cursor:   "",
		Limit:    1,
		MaxBytes: cursorBoundaryMaxBytes,
	}
	// Act: each fresh transaction resumes using only the prior issued cursor.
	got := traverseBoundaryCursors(t, store, scope, options)
	// Assert: all keys occur once, in exact bytewise order, even at the wire bound.
	if !slices.Equal(got, keys) {
		t.Fatal("issued cursor traversal changed keys/order")
	}
	// Arrange / Act / Assert: maximum prefix itself is valid and needs no continuation.
	options.Prefix = keys[0]
	options.After = ""
	must(t, store.View(t.Context(), scope, func(bucket memy.Bucket) error {
		page, err := bucket.Scan(options)
		if err != nil {
			return err
		}
		if len(page.Entries) != 1 || page.Entries[0].Key != keys[0] || !page.Complete {
			t.Fatal("maximum prefix failed")
		}
		return nil
	}))
}

func traverseBoundaryCursors(t *testing.T, store memy.Store, scope memy.Scope, options memy.ScanOptions) []string {
	t.Helper()
	var keys []string
	for {
		var page memy.ScanPage
		must(t, store.View(t.Context(), scope, func(bucket memy.Bucket) error {
			var err error
			page, err = bucket.Scan(options)
			return err
		}))
		for _, entry := range page.Entries {
			keys = append(keys, entry.Key)
		}
		if page.Complete {
			return keys
		}
		if len(page.Entries) == 0 || len(page.Cursor) != cursorEncodedMaximum {
			t.Fatalf("unexpected cursor size: %d", len(page.Cursor))
		}
		rejectChangedBoundaryCursor(t, store, scope, options, page.Cursor)
		options.Cursor = page.Cursor
		options.MaxBytes = cursorBoundaryMaxBytes + 1 // Transport budget is not identity.
	}
}

func rejectChangedBoundaryCursor(
	t *testing.T,
	store memy.Store,
	scope memy.Scope,
	options memy.ScanOptions,
	cursor string,
) {
	t.Helper()
	options.Cursor = cursor
	for _, change := range []func(*memy.ScanOptions){
		func(o *memy.ScanOptions) { o.Plan = "changed" },
		func(o *memy.ScanOptions) { o.After = "" },
		func(o *memy.ScanOptions) { o.Prefix = "changed" },
	} {
		changed := options
		change(&changed)
		must(t, store.View(t.Context(), scope, func(bucket memy.Bucket) error {
			_, err := bucket.Scan(changed)
			if !errors.Is(err, memy.ErrStaleCursor) {
				t.Fatalf("changed binding: %v", err)
			}
			return nil
		}))
	}
	oversized := options
	oversized.Cursor = strings.Repeat("a", cursorEncodedMaximum+1)
	must(t, store.View(t.Context(), scope, func(bucket memy.Bucket) error {
		_, err := bucket.Scan(oversized)
		if !errors.Is(err, memy.ErrInvalid) {
			t.Fatalf("oversized cursor: %v", err)
		}
		return nil
	}))
}
