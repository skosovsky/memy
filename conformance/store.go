// Package conformance supplies reusable behavioral suites for memy adapters.
package conformance

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

const originalPayload = "original"
const scanFixtureKeys = 700
const boundedScanLimit = 37
const boundedScanMaxBytes = 200
const conformanceScanPageLimit = 256
const cursorScanMaxBytes = 100
const oversizedScanLimit = 1025

const lockWaitDeadline = 30 * time.Millisecond
const versionAfterRecreation memy.Version = 3

// Factory creates an isolated store. The suite closes it with t.Cleanup.
type Factory func(*testing.T) memy.Store

// StoreSuite verifies observable transactional, CAS and ownership contracts.
func StoreSuite(t *testing.T, factory Factory) {
	t.Helper()
	cases := []struct {
		name  string
		check func(*testing.T, memy.Store)
	}{
		{"capabilities", capabilities},
		{"scope_isolation", scopeIsolation},
		{"malformed_scope_identity", malformedScopeIdentity},
		{"rollback", rollback},
		{"panic_rollback", panicRollback},
		{"cancel_rollback", cancelRollback},
		{"detached_bytes", detachedBytes},
		{"read_only_and_escaped_bucket", readOnly},
		{"delete_aba_fence", deleteABA},
		{"concurrent_cas", concurrentCAS},
		{"context_aware_wait", contextWait},
		{"malformed_input", malformedInput},
		{"closed", closed},
		{"bounded_scan", boundedScan},
		{"scan_cursor_binding", scanCursorBinding},
		{"scan_cursor_boundaries", scanCursorBoundaries},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			store := factory(t)
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Errorf("close: %v", err)
				}
			})
			test.check(t, store)
		})
	}
}

func boundedScan(t *testing.T, store memy.Store) {
	// Arrange: ordered live keys, a different prefix, and many retained tombstones.
	seedBoundedScan(t, store)
	options := memy.ScanOptions{
		Prefix:   "prefix/",
		Limit:    boundedScanLimit,
		MaxBytes: boundedScanMaxBytes,
		After:    "",
		Plan:     "",
		Cursor:   "",
	}
	var keys []string
	// Act: continue across independent consistent transactions without mutation.
	for {
		var page memy.ScanPage
		must(t, store.View(context.Background(), scope(), func(b memy.Bucket) error {
			var err error
			page, err = b.Scan(options)
			return err
		}))
		// Assert: work/output limits, exact order, and detached value ownership.
		if len(page.Entries) > options.Limit || page.Bytes > options.MaxBytes {
			t.Fatalf("unbounded page: %+v", page)
		}
		for _, entry := range page.Entries {
			keys = append(keys, entry.Key)
			entry.Value.Data[0] = 'X'
		}
		if page.Complete {
			if page.Cursor != "" {
				t.Fatal("complete cursor")
			}
			break
		}
		if page.Cursor == "" || len(page.Entries) == 0 {
			t.Fatal("false progress")
		}
		options.Cursor = page.Cursor
	}
	if len(keys) != scanFixtureKeys {
		t.Fatalf("keys=%d", len(keys))
	}
	for i, key := range keys {
		if key != "prefix/"+fmt.Sprintf("%04d", i) {
			t.Fatalf("unordered key %d: %s", i, key)
		}
	}
	checkBoundedScanOwnershipAndBudgets(t, store, keys)
}

func scanCursorBinding(t *testing.T, store memy.Store) {
	// Arrange: a continuation cursor bound to the exact current scope generation.
	must(t, store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		for _, key := range []string{"a/1", "a/2", "a/3"} {
			if _, err := b.Put(key, 0, []byte("v")); err != nil {
				return err
			}
		}
		return nil
	}))
	options := memy.ScanOptions{Prefix: "a/", Limit: 1, MaxBytes: cursorScanMaxBytes, After: "", Plan: "", Cursor: ""}
	must(
		t,
		store.View(
			context.Background(),
			scope(),
			func(b memy.Bucket) error { page, err := b.Scan(options); options.Cursor = page.Cursor; return err },
		),
	)
	if options.Cursor == "" {
		t.Fatal("missing continuation")
	}
	// Act/Assert: prefix/scope mismatch, tampering, rollback and mutation boundaries.
	changed := options
	changed.Prefix = "b/"
	must(t, store.View(context.Background(), scope(), func(b memy.Bucket) error {
		if _, err := b.Scan(changed); !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("prefix: %v", err)
		}
		return nil
	}))
	other := scope()
	other.Subject = "other"
	must(t, store.View(context.Background(), other, func(b memy.Bucket) error {
		if _, err := b.Scan(options); !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("scope: %v", err)
		}
		return nil
	}))
	rollbackErr := errors.New("rollback")
	if err := store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		if _, err := b.Put("a/4", 0, []byte("v")); err != nil {
			return err
		}
		return rollbackErr
	}); !errors.Is(err, rollbackErr) {
		t.Fatal(err)
	}
	must(
		t,
		store.View(context.Background(), scope(), func(b memy.Bucket) error { _, err := b.Scan(options); return err }),
	)
	must(t, store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		if _, err := b.Put("unrelated", 0, []byte("v")); err != nil {
			return err
		}
		if _, err := b.Scan(options); !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("transaction cursor: %v", err)
		}
		return nil
	}))
	var escaped memy.Bucket
	must(t, store.View(context.Background(), scope(), func(b memy.Bucket) error {
		escaped = b
		if _, err := b.Scan(options); !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("stale cursor: %v", err)
		}
		return nil
	}))
	if _, err := escaped.Scan(options); !errors.Is(err, memy.ErrClosed) {
		t.Fatalf("escaped scan: %v", err)
	}
}

func scope() memy.Scope { return memy.Scope{Tenant: "A", Namespace: "prefs", Subject: "user"} }

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func writeRecord(t *testing.T, store memy.Store, sc memy.Scope, expected memy.Version, value string) {
	t.Helper()
	must(t, store.Update(context.Background(), sc, func(b memy.Bucket) error {
		_, transactionErr := b.Put("record", expected, []byte(value))
		return transactionErr
	}))
}

func get(t *testing.T, store memy.Store, sc memy.Scope, key string) memy.Value {
	t.Helper()
	var result memy.Value
	must(t, store.View(context.Background(), sc, func(b memy.Bucket) error {
		value, transactionErr := b.Get(key)
		result = value
		return transactionErr
	}))
	return result
}

func capabilities(t *testing.T, store memy.Store) {
	// Arrange / Act.
	caps := store.Capabilities()
	// Assert.
	if !caps.Atomic || !caps.ConditionalWrite || !caps.FencedView || caps.SchemaVersion != memy.SchemaVersion {
		t.Fatalf("capabilities: %+v", caps)
	}
}

func scopeIsolation(t *testing.T, store memy.Store) {
	// Arrange: ambiguous delimiter tuples must still be distinct.
	a := memy.Scope{Tenant: "A/B", Namespace: "C", Subject: "D"}
	b := memy.Scope{Tenant: "A", Namespace: "B/C", Subject: "D"}
	writeRecord(t, store, a, 0, "private")
	// Act.
	value := get(t, store, b, "record")
	// Assert.
	if value.Version != 0 || value.Data != nil {
		t.Fatalf("scope leaked: %+v", value)
	}
}

func rollback(t *testing.T, store memy.Store) {
	// Arrange.
	writeRecord(t, store, scope(), 0, originalPayload)
	failure := errors.New("injected failure")
	// Act: the record and its receipt are one transaction.
	operationErr := store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		if _, err := b.Put("record", 1, []byte("changed")); err != nil {
			return err
		}
		if _, err := b.Put("receipt", 0, []byte("committed")); err != nil {
			return err
		}
		return failure
	})
	// Assert.
	if !errors.Is(operationErr, failure) {
		t.Fatalf("want failure: %v", operationErr)
	}
	if value := get(t, store, scope(), "record"); value.Version != 1 || string(value.Data) != originalPayload {
		t.Fatalf("rollback record: %+v", value)
	}
	if value := get(t, store, scope(), "receipt"); value.Version != 0 {
		t.Fatalf("receipt escaped: %+v", value)
	}
}

func panicRollback(t *testing.T, store memy.Store) {
	// Arrange.
	recovered := false
	// Act.
	func() {
		defer func() { recovered = recover() == "fault" }()
		_ = store.Update(context.Background(), scope(), func(b memy.Bucket) error {
			if _, err := b.Put("record", 0, []byte("private")); err != nil {
				return err
			}
			panic("fault")
		})
	}()
	// Assert: panic propagates and store remains usable.
	if !recovered {
		t.Fatal("panic swallowed")
	}
	if value := get(t, store, scope(), "record"); value.Version != 0 {
		t.Fatal("panic committed")
	}
	writeRecord(t, store, scope(), 0, "recovered")
}

func cancelRollback(t *testing.T, store memy.Store) {
	// Arrange.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Act.
	operationErr := store.Update(ctx, scope(), func(b memy.Bucket) error {
		if _, err := b.Put("record", 0, []byte("payload")); err != nil {
			return err
		}
		cancel()
		return nil
	})
	// Assert.
	if !errors.Is(operationErr, context.Canceled) {
		t.Fatalf("cancel lost: %v", operationErr)
	}
	if value := get(t, store, scope(), "record"); value.Version != 0 {
		t.Fatal("canceled transaction committed")
	}
}

func detachedBytes(t *testing.T, store memy.Store) {
	// Arrange.
	input := []byte(originalPayload)
	must(t, store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		if _, err := b.Put("record", 0, input); err != nil {
			return err
		}
		input[0] = 'X'
		return nil
	}))
	// Act: mutate Get and List results.
	value := get(t, store, scope(), "record")
	value.Data[0] = 'Y'
	must(t, store.View(context.Background(), scope(), func(b memy.Bucket) error {
		entries, transactionErr := entries(b, "")
		if transactionErr != nil {
			return transactionErr
		}
		entries[0].Value.Data[0] = 'Z'
		return nil
	}))
	// Assert.
	if value := get(t, store, scope(), "record"); string(value.Data) != originalPayload {
		t.Fatalf("alias changed store: %s", value.Data)
	}
}

func readOnly(t *testing.T, store memy.Store) {
	// Arrange.
	var escaped memy.Bucket
	var putErr error
	// Act.
	must(t, store.View(context.Background(), scope(), func(b memy.Bucket) error {
		escaped = b
		_, putErr = b.Put("record", 0, []byte("bad"))
		return nil
	}))
	_, escapedErr := escaped.Get("record")
	// Assert.
	if !errors.Is(putErr, memy.ErrUnsupported) || !errors.Is(escapedErr, memy.ErrClosed) {
		t.Fatalf("read-only=%v escaped=%v", putErr, escapedErr)
	}
}

func deleteABA(t *testing.T, store memy.Store) {
	// Arrange.
	writeRecord(t, store, scope(), 0, originalPayload)
	// Act.
	must(t, store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		_, transactionErr := b.Delete("record", 1)
		return transactionErr
	}))
	tombstone := get(t, store, scope(), "record")
	operationErr := store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		_, transactionErr := b.Put("record", 0, []byte("stale"))
		return transactionErr
	})
	// Assert.
	if tombstone.Version != 2 || tombstone.Data != nil || !errors.Is(operationErr, memy.ErrConflict) {
		t.Fatalf("ABA: %+v, %v", tombstone, operationErr)
	}
	writeRecord(t, store, scope(), 2, "fresh")
	if value := get(t, store, scope(), "record"); value.Version != versionAfterRecreation {
		t.Fatalf("version reset: %d", value.Version)
	}
}

func concurrentCAS(t *testing.T, store memy.Store) {
	// Arrange.
	writeRecord(t, store, scope(), 0, originalPayload)
	start := make(chan struct{})
	errorsOut := make(chan error, 2)
	var writers sync.WaitGroup
	// Act.
	for i := range 2 {
		writers.Go(func() {
			<-start
			errorsOut <- store.Update(context.Background(), scope(), func(b memy.Bucket) error {
				_, transactionErr := b.Put("record", 1, []byte(strconv.Itoa(i)))
				return transactionErr
			})
		})
	}
	close(start)
	writers.Wait()
	close(errorsOut)
	// Assert.
	winners, conflicts := 0, 0
	for err := range errorsOut {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, memy.ErrConflict):
			conflicts++
		default:
			t.Fatalf("unexpected writer error: %v", err)
		}
	}
	if winners != 1 || conflicts != 1 || get(t, store, scope(), "record").Version != 2 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
}

func contextWait(t *testing.T, store memy.Store) {
	// Arrange: hold exact-scope mutation exclusion.
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- store.Update(context.Background(), scope(), func(memy.Bucket) error { close(entered); <-release; return nil })
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), lockWaitDeadline)
	defer cancel()
	// Act.
	operationErr := store.FencedView(ctx, scope(), func(memy.Bucket) error { return errors.New("must not enter") })
	close(release)
	otherErr := <-done
	// Assert.
	if !errors.Is(operationErr, context.DeadlineExceeded) {
		t.Fatalf("wait did not honor deadline: %v", operationErr)
	}
	must(t, otherErr)
}

func malformedInput(t *testing.T, store memy.Store) {
	// Arrange / Act.
	badScope := store.View(
		context.Background(),
		memy.Scope{Tenant: "", Namespace: "", Subject: ""},
		func(memy.Bucket) error { return nil },
	)
	badCallback := store.Update(context.Background(), scope(), nil)
	var empty, key, overflow error
	must(t, store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		_, empty = b.Put("record", 0, nil)
		_, key = b.Put("", 0, []byte("bad"))
		_, overflow = b.Put("record", memy.MaxVersion, []byte("bad"))
		return nil
	}))
	// Assert.
	for _, err := range []error{badScope, badCallback, empty, key} {
		if !errors.Is(err, memy.ErrInvalid) {
			t.Fatalf("invalid accepted: %v", err)
		}
	}
	if !errors.Is(overflow, memy.ErrConflict) {
		t.Fatalf("invalid version: %v", overflow)
	}
}

func closed(t *testing.T, store memy.Store) {
	// Arrange.
	must(t, store.Close())
	// Act.
	operationErr := store.View(context.Background(), scope(), func(memy.Bucket) error { return nil })
	// Assert.
	if operationErr == nil {
		t.Fatal("closed store accepted operation")
	}
	must(t, store.Close())
}

func malformedScopeIdentity(t *testing.T, store memy.Store) {
	// Arrange: a valid replacement rune must remain distinct from malformed bytes.
	good := memy.Scope{Tenant: "\ufffd", Namespace: "имя", Subject: "👩"}
	writeRecord(t, store, good, 0, originalPayload)
	for _, component := range []string{"tenant", "namespace", "subject"} {
		for _, malformed := range []string{string([]byte{0xff}), string([]byte{0xfe}), "x\x00y"} {
			bad := good
			switch component {
			case "tenant":
				bad.Tenant = malformed
			case "namespace":
				bad.Namespace = malformed
			case "subject":
				bad.Subject = malformed
			}
			// Act: neither read nor write may enter the bucket.
			calls := 0
			callback := func(memy.Bucket) error { calls++; return nil }
			readErr := store.View(t.Context(), bad, callback)
			writeErr := store.Update(t.Context(), bad, callback)
			// Assert: invalid scope is rejected, existing state is unchanged.
			if !errors.Is(readErr, memy.ErrInvalid) || !errors.Is(writeErr, memy.ErrInvalid) || calls != 0 {
				t.Fatalf("%s: read=%v write=%v calls=%d", component, readErr, writeErr, calls)
			}
		}
	}
	if value := get(t, store, good, "record"); string(value.Data) != originalPayload || value.Version != 1 {
		t.Fatal("invalid scope mutated valid state")
	}
}

func entries(b memy.Bucket, prefix string) ([]memy.Entry, error) {
	var result []memy.Entry
	options := memy.ScanOptions{
		Prefix:   prefix,
		Limit:    conformanceScanPageLimit,
		MaxBytes: int(^uint(0) >> 1),
		After:    "",
		Plan:     "",
		Cursor:   "",
	}
	for {
		page, err := b.Scan(options)
		if err != nil {
			return nil, err
		}
		result = append(result, page.Entries...)
		if page.Complete {
			return result, nil
		}
		if page.Cursor == "" || len(page.Entries) == 0 {
			return nil, memy.ErrSchema
		}
		options.Cursor = page.Cursor
	}
}

func seedBoundedScan(t *testing.T, store memy.Store) {
	t.Helper()
	must(t, store.Update(context.Background(), scope(), func(b memy.Bucket) error {
		for i := range scanFixtureKeys {
			key := "prefix/" + fmt.Sprintf("%04d", i)
			if _, err := b.Put(key, 0, []byte("payload")); err != nil {
				return err
			}
		}
		for i := range 1000 {
			if _, err := b.Delete("deleted/"+strconv.Itoa(i), 0); err != nil {
				return err
			}
		}
		_, err := b.Put("other/key", 0, []byte("excluded"))
		return err
	}))
}

func checkBoundedScanOwnershipAndBudgets(t *testing.T, store memy.Store, keys []string) {
	t.Helper()
	must(t, store.View(context.Background(), scope(), func(b memy.Bucket) error {
		v, err := b.Get(keys[0])
		if err != nil {
			return err
		}
		if string(v.Data) != "payload" {
			t.Fatal("scan aliases stored bytes")
		}
		for _, options := range []memy.ScanOptions{{Limit: 0, MaxBytes: 1, Prefix: "", After: "", Plan: "", Cursor: ""}, {Limit: oversizedScanLimit, MaxBytes: 1, Prefix: "", After: "", Plan: "", Cursor: ""}, {Limit: 1, MaxBytes: 0, Prefix: "", After: "", Plan: "", Cursor: ""}} {
			if _, err := b.Scan(options); !errors.Is(err, memy.ErrInvalid) {
				t.Fatalf("invalid scan: %v", err)
			}
		}
		if _, err := b.Scan(
			memy.ScanOptions{Prefix: "prefix/", Limit: 1, MaxBytes: 1, After: "", Plan: "", Cursor: ""},
		); !errors.Is(
			err,
			memy.ErrBudget,
		) {
			t.Fatalf("oversized entry: %v", err)
		}
		return nil
	}))
}
