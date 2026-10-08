package sqlite_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestCursorSurvivesHandleReopenButNotMutationOrDatabaseCopy(t *testing.T) {
	// Arrange: a cursor does not retain its originating database handle.
	path := filepath.Join(t.TempDir(), "cursor.db")
	a := open(t, path, sqlite.Options{})
	if err := a.Update(context.Background(), testScope(), func(b memy.Bucket) error {
		for _, key := range []string{"prefix/a", "prefix/b"} {
			if _, err := b.Put(key, 0, []byte("v")); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	options := memy.ScanOptions{Prefix: "prefix/", Limit: 1, MaxBytes: 100}
	if err := a.View(
		context.Background(),
		testScope(),
		func(b memy.Bucket) error { p, err := b.Scan(options); options.Cursor = p.Cursor; return err },
	); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// Act/Assert: reopen binds the same physical database and persisted generation.
	b := open(t, path, sqlite.Options{})
	if err := b.View(context.Background(), testScope(), func(bucket memy.Bucket) error {
		p, err := bucket.Scan(options)
		if err == nil && (len(p.Entries) != 1 || p.Entries[0].Key != "prefix/b" || !p.Complete) {
			t.Fatalf("resume: %+v", p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	checkCopiedDatabaseCursor(t, path, options)
	c := open(t, path, sqlite.Options{})
	if err := c.Update(
		context.Background(),
		testScope(),
		func(bucket memy.Bucket) error { _, err := bucket.Delete("prefix/b", 1); return err },
	); err != nil {
		t.Fatal(err)
	}
	if err := c.View(context.Background(), testScope(), func(bucket memy.Bucket) error {
		_, err := bucket.Scan(options)
		if !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("stale cursor: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func checkCopiedDatabaseCursor(t *testing.T, path string, options memy.ScanOptions) {
	t.Helper()
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(copyPath, raw, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	copied := open(t, copyPath, sqlite.Options{})
	if viewErr := copied.View(context.Background(), testScope(), func(bucket memy.Bucket) error {
		_, err := bucket.Scan(options)
		if !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("copy cursor: %v", err)
		}
		return nil
	}); viewErr != nil {
		t.Fatal(viewErr)
	}
}
