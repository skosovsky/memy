// Package sqlite supplies a durable local store using SQLite transactions.
// It requires CGO and a C compiler. Core and memory do not require this package.
package sqlite

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mattn/go-sqlite3" // Registers the optional SQLite driver.

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/internal/kv"
)

// Stage identifies a fault-injection boundary. Hooks execute synchronously.
type Stage string

const lockWaitLimit = 5 * time.Second
const lockRetryDelay = 2 * time.Millisecond

const (
	// BeforeCommit tests rollback of all data and receipts.
	BeforeCommit Stage = "before_commit"
	// AfterCommit tests an ambiguous response after a durable transaction.
	AfterCommit Stage = "after_commit"
)

// Options configures the reference adapter. Fault must be concurrency-safe.
type Options struct {
	Fault func(context.Context, Stage) error
}

// Store owns a SQLite connection pool. Callbacks are transaction-local.
type Store struct {
	db       *sql.DB
	reads    *sql.DB
	fenceDir string
	fault    func(context.Context, Stage) error
	once     sync.Once
	closeErr error
	secret   []byte
	closed   atomic.Bool
}

// Open initializes or validates schema v3. Path must refer to a real local
// file; temporary/in-memory databases would violate Durable capabilities.
func Open(ctx context.Context, path string, options Options) (*Store, error) {
	if path == "" || path == ":memory:" {
		return nil, memy.ErrInvalid
	}
	if !platformFencing {
		return nil, memy.ErrUnsupported
	}
	absolute, operationErr := filepath.Abs(path)
	if operationErr != nil {
		return nil, fmt.Errorf("sqlite path: %w", operationErr)
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute}).String() +
		"?_busy_timeout=25&_journal_mode=WAL&_synchronous=FULL&_txlock=immediate&_foreign_keys=on&_secure_delete=on"
	db, operationErr := sql.Open("sqlite3", dsn)
	if operationErr != nil {
		return nil, fmt.Errorf("sqlite open: %w", errors.Join(memy.ErrUnavailable, operationErr))
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db, fault: options.Fault, once: sync.Once{}, closeErr: nil}
	if err := store.initialize(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		_ = db.Close()
		return nil, storageError(err)
	}
	store.fenceDir = canonical + ".memy-fences"
	identity, err := databaseIdentity(canonical)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	mac := hmac.New(sha256.New, store.secret)
	_, _ = mac.Write([]byte(identity))
	store.secret = mac.Sum(nil)
	if err := os.MkdirAll(store.fenceDir, 0700); err != nil {
		_ = db.Close()
		return nil, storageError(err)
	}
	store.reads, operationErr = sql.Open("sqlite3", strings.Replace(dsn, "_txlock=immediate", "_txlock=deferred", 1))
	if operationErr != nil {
		_ = db.Close()
		return nil, storageError(operationErr)
	}
	store.reads.SetMaxOpenConns(16)
	store.reads.SetMaxIdleConns(4)
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	tx, operationErr := s.begin(ctx)
	if operationErr != nil {
		return storageError(operationErr)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS memy_schema (singleton INTEGER PRIMARY KEY CHECK(singleton=1), version INTEGER NOT NULL, cursor_secret BLOB)`); err != nil {
		return storageError(err)
	}
	inserted, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO memy_schema(singleton,version) VALUES(1,3)`)
	if err != nil {
		return storageError(err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT version FROM memy_schema WHERE singleton=1`).Scan(&version); err != nil {
		return storageError(err)
	}
	if version != int(memy.SchemaVersion) {
		return memy.ErrSchema
	}
	created, err := inserted.RowsAffected()
	if err != nil {
		return storageError(err)
	}
	if created == 1 {
		s.secret = make([]byte, 32)
		if _, err := rand.Read(s.secret); err != nil {
			return storageError(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE memy_schema SET cursor_secret=? WHERE singleton=1`, s.secret); err != nil {
			return storageError(err)
		}
	} else {
		if err := tx.QueryRowContext(ctx, `SELECT cursor_secret FROM memy_schema WHERE singleton=1`).Scan(&s.secret); err != nil {
			return errors.Join(memy.ErrSchema, err)
		}
		if len(s.secret) != 32 {
			return memy.ErrSchema
		}
	}
	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS memy_values (
		scope TEXT NOT NULL, key TEXT NOT NULL, version INTEGER NOT NULL CHECK(version>0),
		value BLOB, PRIMARY KEY(scope,key)) WITHOUT ROWID`); err != nil {
		return storageError(err)
	}
	for _, statement := range []string{
		`CREATE INDEX IF NOT EXISTS memy_live_keys ON memy_values(scope,key) WHERE value IS NOT NULL`,
		`CREATE TABLE IF NOT EXISTS memy_generations (scope TEXT PRIMARY KEY, generation INTEGER NOT NULL CHECK(generation>0)) WITHOUT ROWID`,
	} {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return storageError(err)
		}
	}
	if err := tx.Commit(); err != nil {
		return storageError(err)
	}
	return nil
}

// Capabilities describes the persistent transactional contract.
func (*Store) Capabilities() memy.StoreCapabilities {
	return memy.StoreCapabilities{Atomic: true, ConditionalWrite: true, FencedView: platformFencing, Durable: true, SchemaVersion: memy.SchemaVersion}
}

// View reads addressed values in a consistent transaction, rejecting writes.
func (s *Store) View(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	return s.run(ctx, scope, false, fn)
}

// FencedView uses a scoped cross-process shared lock and a WAL read transaction.
// It does not hold SQLite's global writer reservation during the callback.
func (s *Store) FencedView(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	if fn == nil {
		return memy.ErrInvalid
	}
	release, err := s.scopeFence(ctx, scope, false)
	if err != nil {
		return err
	}
	defer release()
	return s.run(ctx, scope, false, fn)
}

// Update uses BEGIN IMMEDIATE and commits only successful addressed CAS writes.
func (s *Store) Update(ctx context.Context, scope memy.Scope, fn func(memy.Bucket) error) error {
	if fn == nil {
		return memy.ErrInvalid
	}
	release, err := s.scopeFence(ctx, scope, true)
	if err != nil {
		return err
	}
	defer release()
	return s.run(ctx, scope, true, fn)
}

func (s *Store) run(ctx context.Context, scope memy.Scope, writable bool, fn func(memy.Bucket) error) error {
	if err := scope.Validate(); err != nil {
		return err
	}
	if fn == nil {
		return memy.ErrInvalid
	}
	if s.closed.Load() {
		return memy.ErrClosed
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var tx *sql.Tx
	var operationErr error
	if writable {
		tx, operationErr = s.begin(ctx)
	} else {
		tx, operationErr = s.reads.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	}
	if operationErr != nil {
		return storageError(operationErr)
	}
	defer func() { _ = tx.Rollback() }()
	var generation memy.Version
	err := tx.QueryRowContext(ctx, `SELECT generation FROM memy_generations WHERE scope=?`, scope.Key()).Scan(&generation)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return storageError(err)
	}
	if generation > memy.MaxVersion || (err == nil && generation == 0) {
		return memy.ErrSchema
	}
	bucket := &sqlBucket{ctx: ctx, tx: tx, scope: scope.Key(), writable: writable, binding: kv.Binding{Secret: s.secret, Scope: scope.Key(), Generation: generation}}
	defer bucket.Seal()
	if err := fn(bucket); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !writable {
		return nil
	} // Deferred rollback releases the read transaction.
	if bucket.binding.Generation != generation {
		if _, err := tx.ExecContext(ctx, `INSERT INTO memy_generations(scope,generation) VALUES(?,?) ON CONFLICT(scope) DO UPDATE SET generation=excluded.generation`, scope.Key(), bucket.binding.Generation); err != nil {
			return storageError(err)
		}
	}
	if s.fault != nil {
		if err := s.fault(ctx, BeforeCommit); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		// Commit errors may not certify absence of external durable effects.
		return errors.Join(memy.ErrUnknownOutcome, storageError(err))
	}
	if s.fault != nil {
		if err := s.fault(ctx, AfterCommit); err != nil {
			return errors.Join(memy.ErrUnknownOutcome, err)
		}
	}
	return nil
}

func storageError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("sqlite storage: %w", errors.Join(memy.ErrUnavailable, err))
}

func (s *Store) begin(ctx context.Context) (*sql.Tx, error) {
	until := time.Now().Add(lockWaitLimit)
	for {
		tx, err := s.db.BeginTx(ctx, nil)
		if err == nil {
			return tx, nil
		}
		if contextErr := ctx.Err(); contextErr != nil {
			return nil, contextErr
		}
		var busy sqlite3.Error
		if !errors.As(err, &busy) || (busy.Code != sqlite3.ErrBusy && busy.Code != sqlite3.ErrLocked) ||
			!time.Now().Before(until) {
			return nil, storageError(err)
		}
		timer := time.NewTimer(lockRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// Close releases connections. Repeated calls return the first result.
func (s *Store) Close() error {
	s.once.Do(func() { s.closed.Store(true); s.closeErr = errors.Join(s.reads.Close(), s.db.Close()) })
	return s.closeErr
}
