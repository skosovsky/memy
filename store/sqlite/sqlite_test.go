package sqlite_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/conformance"
	"github.com/skosovsky/memy/store/sqlite"
)

func testScope() memy.Scope { return memy.Scope{Tenant: "A", Namespace: "prefs", Subject: "user"} }

func open(t *testing.T, path string, opts sqlite.Options) *sqlite.Store {
	t.Helper()
	store, operationErr := sqlite.Open(context.Background(), path, opts)
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func read(t *testing.T, store *sqlite.Store, key string) memy.Value {
	t.Helper()
	var value memy.Value
	operationErr := store.View(context.Background(), testScope(), func(b memy.Bucket) error {
		result, transactionErr := b.Get(key)
		value = result
		return transactionErr
	})
	if operationErr != nil {
		t.Fatal(operationErr)
	}
	return value
}

func commit(ctx context.Context, store *sqlite.Store, expected memy.Version, payload string) error {
	return store.Update(ctx, testScope(), func(b memy.Bucket) error {
		if _, err := b.Put("record", expected, []byte(payload)); err != nil {
			return err
		}
		_, transactionErr := b.Put("receipt/"+payload, 0, []byte("committed"))
		return transactionErr
	})
}

func TestConformance(t *testing.T) {
	conformance.StoreSuite(t, func(t *testing.T) memy.Store {
		return open(t, filepath.Join(t.TempDir(), "store.db"), sqlite.Options{})
	})
}

func TestDurableReopen(t *testing.T) {
	// Arrange.
	path := filepath.Join(t.TempDir(), "store.db")
	store := open(t, path, sqlite.Options{})
	if err := commit(context.Background(), store, 0, "original"); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	// Act.
	reopened := open(t, path, sqlite.Options{})
	value := read(t, reopened, "record")
	receipt := read(t, reopened, "receipt/original")
	// Assert.
	if value.Version != 1 || string(value.Data) != "original" || string(receipt.Data) != "committed" {
		t.Fatalf("reopen record=%+v receipt=%+v", value, receipt)
	}
}

func TestIndependentConcurrentWriters(t *testing.T) {
	// Arrange: two independently opened connections to the same file.
	path := filepath.Join(t.TempDir(), "store.db")
	a, b := open(t, path, sqlite.Options{}), open(t, path, sqlite.Options{})
	if err := commit(context.Background(), a, 0, "original"); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	results := make(chan error, 2)
	var group sync.WaitGroup
	// Act.
	for i, store := range []*sqlite.Store{a, b} {
		group.Go(func() {
			<-start
			results <- commit(context.Background(), store, 1, []string{"A", "B"}[i])
		})
	}
	close(start)
	group.Wait()
	close(results)
	// Assert.
	winners, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			winners++
		case errors.Is(err, memy.ErrConflict):
			conflicts++
		default:
			t.Fatal(err)
		}
	}
	if winners != 1 || conflicts != 1 || read(t, a, "record").Version != 2 {
		t.Fatalf("winners=%d conflicts=%d", winners, conflicts)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := open(t, path, sqlite.Options{})
	if read(t, reopened, "record").Version != 2 {
		t.Fatal("winning revision lost on reopen")
	}
}

func TestFaultRecovery(t *testing.T) {
	for _, stage := range []sqlite.Stage{sqlite.BeforeCommit, sqlite.AfterCommit} {
		t.Run(string(stage), func(t *testing.T) { faultRecoveryStage(t, stage) })
	}
}

func faultRecoveryStage(t *testing.T, stage sqlite.Stage) {
	t.Helper()

	// Arrange.
	path := filepath.Join(t.TempDir(), "store.db")
	failure := errors.New("injected durable fault")
	var fired atomic.Bool
	store := open(t, path, sqlite.Options{Fault: func(_ context.Context, at sqlite.Stage) error {
		if at == stage && fired.CompareAndSwap(false, true) {
			return failure
		}
		return nil
	}})
	// Act.
	transactionErr := commit(context.Background(), store, 0, "payload")
	if closeErr := store.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	reopened := open(t, path, sqlite.Options{})
	value, receipt := read(t, reopened, "record"), read(t, reopened, "receipt/payload")
	// Assert: both sides survive, or neither does.
	if !errors.Is(transactionErr, failure) {
		t.Fatalf("fault missing: %v", transactionErr)
	}
	if stage == sqlite.BeforeCommit {
		if value.Version != 0 || receipt.Version != 0 {
			t.Fatal("partial/failed commit persisted")
		}
		if retryErr := commit(context.Background(), reopened, 0, "payload"); retryErr != nil {
			t.Fatal(retryErr)
		}
	} else if !errors.Is(transactionErr, memy.ErrUnknownOutcome) || value.Version != 1 || receipt.Version != 1 {
		t.Fatalf("unknown outcome=%v record=%+v receipt=%+v", transactionErr, value, receipt)
	}
}

func TestRejectIncompatibleSchema(t *testing.T) {
	for _, version := range []int{1, 3} {
		t.Run(strconv.Itoa(version), func(t *testing.T) {
			// Arrange.
			path := filepath.Join(t.TempDir(), "store.db")
			store := open(t, path, sqlite.Options{})
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			db, operationErr := sql.Open("sqlite3", path)
			if operationErr != nil {
				t.Fatal(operationErr)
			}
			if _, err := db.Exec(`UPDATE memy_schema SET version=?`, version); err != nil {
				t.Fatal(err)
			}
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			// Act.
			result, operationErr := sqlite.Open(context.Background(), path, sqlite.Options{})
			// Assert.
			if result != nil || !errors.Is(operationErr, memy.ErrSchema) {
				t.Fatalf("newer schema accepted: %v", operationErr)
			}
		})
	}
}

func TestIndependentWriterCancellationAndRecovery(t *testing.T) {
	// Arrange: one handle holds the SQLite write lock; the other is independent.
	path := filepath.Join(t.TempDir(), "store.db")
	a, b := open(t, path, sqlite.Options{}), open(t, path, sqlite.Options{})
	entered, release := make(chan struct{}), make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- a.Update(context.Background(), testScope(), func(bucket memy.Bucket) error {
			close(entered)
			<-release
			_, transactionErr := bucket.Put("winner", 0, []byte("committed"))
			return transactionErr
		})
	}()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	// Act.
	operationErr := b.Update(
		ctx,
		testScope(),
		func(bucket memy.Bucket) error {
			_, transactionErr := bucket.Put("canceled", 0, []byte("bad"))
			return transactionErr
		},
	)
	elapsed := time.Since(start)
	close(release)
	winnerErr := <-writerDone
	// Assert: cancellation is a context error, not a fake unavailable/commit.
	if !errors.Is(operationErr, context.DeadlineExceeded) || elapsed > time.Second || winnerErr != nil {
		t.Fatalf("cancel=%v elapsed=%v winner=%v", operationErr, elapsed, winnerErr)
	}
	if read(t, b, "canceled").Version != 0 || string(read(t, b, "winner").Data) != "committed" {
		t.Fatal("canceled contention altered durable state")
	}
	if retryErr := commit(context.Background(), b, 0, "later"); retryErr != nil {
		t.Fatalf("connection unusable after cancel: %v", retryErr)
	}
}
