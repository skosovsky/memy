// Package checkpoint is a host-owned durable checkpoint example, not core policy.
package checkpoint

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/url"
	"path/filepath"
	"slices"
	"sync"

	_ "github.com/mattn/go-sqlite3" // Registers the optional example driver.

	"github.com/skosovsky/memy"
)

// Artifact holds exact lineage and host representation; it is not authority.
type Artifact struct {
	Scope   memy.Scope         `json:"scope"`
	Handle  string             `json:"handle"`
	Lineage []memy.RevisionRef `json:"lineage"`
	Data    []byte             `json:"data"`
}

// Store owns one local SQLite checkpoint database, separate from canonical data.
type Store struct {
	db      *sql.DB
	name    string
	mu      sync.Mutex
	failure error
}

// Open enables transactional lineage, epoch fences and durable purge.
func Open(ctx context.Context, path, name string) (*Store, error) {
	if path == "" || path == ":memory:" || name == "" {
		return nil, memy.ErrInvalid
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: absolute}).String() + "?_busy_timeout=5000&_journal_mode=WAL&_synchronous=FULL&_txlock=immediate&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, name: name, mu: sync.Mutex{}, failure: nil}
	_, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS artifacts (
 scope TEXT NOT NULL, key TEXT NOT NULL, handle TEXT NOT NULL, data BLOB NOT NULL, epoch INTEGER NOT NULL,
 PRIMARY KEY(scope,key));
 CREATE TABLE IF NOT EXISTS lineage (
 scope TEXT NOT NULL, key TEXT NOT NULL, record TEXT NOT NULL, revision INTEGER NOT NULL,
 PRIMARY KEY(scope,key,record,revision),
 FOREIGN KEY(scope,key) REFERENCES artifacts(scope,key) ON DELETE CASCADE);
 CREATE INDEX IF NOT EXISTS reverse_lineage ON lineage(scope,record);
 CREATE TABLE IF NOT EXISTS epochs (scope TEXT PRIMARY KEY, epoch INTEGER NOT NULL);`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Name is the stable sink identity in canonical purge receipts.
func (s *Store) Name() string { return s.name }

// Close closes this example's database pool.
func (s *Store) Close() error { return s.db.Close() }

// FailPurge is a synchronized example-only failure injection hook.
func (s *Store) FailPurge(err error) { s.mu.Lock(); defer s.mu.Unlock(); s.failure = err }

// Key structurally binds a handle to its scope and all exact inputs.
func Key(a Artifact) (string, error) {
	if a.Scope.Validate() != nil || a.Handle == "" || len(a.Lineage) == 0 || len(a.Lineage) > memy.MaxSearchCandidates {
		return "", memy.ErrInvalid
	}
	refs := slices.Clone(a.Lineage)
	slices.SortFunc(refs, func(a, b memy.RevisionRef) int {
		if a.RecordID < b.RecordID {
			return -1
		}
		if a.RecordID > b.RecordID {
			return 1
		}
		if a.Revision < b.Revision {
			return -1
		}
		if a.Revision > b.Revision {
			return 1
		}
		return 0
	})
	for i, ref := range refs {
		if ref.Validate() != nil || (i > 0 && ref == refs[i-1]) {
			return "", memy.ErrInvalid
		}
	}
	raw, err := json.Marshal(struct {
		Scope   memy.Scope         `json:"scope"`
		Handle  string             `json:"handle"`
		Lineage []memy.RevisionRef `json:"lineage"`
	}{a.Scope, a.Handle, refs})
	return string(raw), err
}

// Put commits data and reverse lineage together. Invoke only in WithDerivedWrite.
// The sink epoch rejects stale remote retries even after a canonical gate returns.
func (s *Store) Put(ctx context.Context, fence memy.EpochFence, a Artifact) (string, error) {
	key, err := Key(a)
	if err != nil {
		return "", err
	}
	if a.Scope != fence.Scope || fence.Epoch > memy.MaxVersion || len(a.Data) == 0 {
		return "", memy.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var epoch memy.Version
	err = tx.QueryRowContext(ctx, "SELECT epoch FROM epochs WHERE scope=?", a.Scope.Key()).Scan(&epoch)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if fence.Epoch < epoch {
		return "", memy.ErrStaleInput
	}
	if _, err = tx.ExecContext(
		ctx,
		"INSERT INTO epochs(scope,epoch) VALUES(?,?) ON CONFLICT(scope) DO UPDATE SET epoch=MAX(epoch,excluded.epoch)",
		a.Scope.Key(),
		fence.Epoch,
	); err != nil {
		return "", err
	}
	// Identical retry is allowed; conflicting content under one exact handle is not.
	var existing []byte
	err = tx.QueryRowContext(ctx, "SELECT data FROM artifacts WHERE scope=? AND key=?", a.Scope.Key(), key).
		Scan(&existing)
	if err == nil {
		if !bytes.Equal(existing, a.Data) {
			return "", memy.ErrConflict
		}
		return key, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if _, err = tx.ExecContext(
		ctx,
		"INSERT INTO artifacts(scope,key,handle,data,epoch) VALUES(?,?,?,?,?)",
		a.Scope.Key(),
		key,
		a.Handle,
		a.Data,
		fence.Epoch,
	); err != nil {
		return "", err
	}
	for _, ref := range a.Lineage {
		if _, err = tx.ExecContext(
			ctx,
			"INSERT INTO lineage(scope,key,record,revision) VALUES(?,?,?,?)",
			a.Scope.Key(),
			key,
			ref.RecordID,
			ref.Revision,
		); err != nil {
			return "", err
		}
	}
	return key, tx.Commit()
}

// Load is privileged host storage access. Validate lineage before delivery.
func (s *Store) Load(ctx context.Context, scope memy.Scope, key string) (Artifact, error) {
	if scope.Validate() != nil || key == "" {
		return Artifact{}, memy.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelDefault, ReadOnly: true})
	if err != nil {
		return Artifact{}, err
	}
	defer func() { _ = tx.Rollback() }()
	a := Artifact{Scope: scope, Handle: "", Lineage: nil, Data: nil}
	if err = tx.QueryRowContext(ctx, "SELECT handle,data FROM artifacts WHERE scope=? AND key=?", scope.Key(), key).
		Scan(&a.Handle, &a.Data); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Artifact{}, memy.ErrNotFound
		}
		return Artifact{}, err
	}
	rows, err := tx.QueryContext(
		ctx,
		"SELECT record,revision FROM lineage WHERE scope=? AND key=? ORDER BY record,revision",
		scope.Key(),
		key,
	)
	if err != nil {
		return Artifact{}, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var ref memy.RevisionRef
		if err = rows.Scan(&ref.RecordID, &ref.Revision); err != nil {
			return Artifact{}, err
		}
		a.Lineage = append(a.Lineage, ref)
	}
	scanErr := rows.Err()
	closeErr := rows.Close()
	if err = errors.Join(scanErr, closeErr); err != nil {
		return Artifact{}, err
	}
	if expected, validation := Key(a); validation != nil || expected != key {
		return Artifact{}, memy.ErrSchema
	}
	if err = tx.Commit(); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

// Invalidate removes only the exact scoped checkpoint and all its lineage rows.
func (s *Store) Invalidate(ctx context.Context, scope memy.Scope, key string) error {
	if scope.Validate() != nil {
		return memy.ErrInvalid
	}
	_, err := s.db.ExecContext(ctx, "DELETE FROM artifacts WHERE scope=? AND key=?", scope.Key(), key)
	return err
}

// Purge acknowledges only after durable deletion and a monotonic sink fence.
func (s *Store) Purge(ctx context.Context, batch memy.PurgeBatch) (memy.PurgeAck, error) {
	s.mu.Lock()
	failure := s.failure
	s.mu.Unlock()
	if failure != nil {
		return memy.PurgeAck{}, failure
	}
	if batch.Scope.Validate() != nil || batch.Epoch == 0 || batch.Epoch > memy.MaxVersion || batch.OperationID == "" {
		return memy.PurgeAck{}, memy.ErrInvalid
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memy.PurgeAck{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(
		ctx,
		"INSERT INTO epochs(scope,epoch) VALUES(?,?) ON CONFLICT(scope) DO UPDATE SET epoch=MAX(epoch,excluded.epoch)",
		batch.Scope.Key(),
		batch.Epoch,
	); err != nil {
		return memy.PurgeAck{}, err
	}
	switch batch.Selector.Kind {
	case memy.SelectScope, memy.SelectSubject:
		if batch.Selector.Kind == memy.SelectSubject && batch.Selector.ID != batch.Scope.Subject {
			return memy.PurgeAck{}, memy.ErrInvalid
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM artifacts WHERE scope=? AND epoch<?", batch.Scope.Key(), batch.Epoch)
	case memy.SelectRecord, memy.SelectSource:
		for _, id := range batch.Records {
			if _, err = tx.ExecContext(
				ctx,
				"DELETE FROM artifacts WHERE scope=? AND epoch<? AND key IN (SELECT key FROM lineage WHERE scope=? AND record=?)",
				batch.Scope.Key(),
				batch.Epoch,
				batch.Scope.Key(),
				id,
			); err != nil {
				break
			}
		}
	default:
		return memy.PurgeAck{}, memy.ErrInvalid
	}
	if err != nil {
		return memy.PurgeAck{}, err
	}
	if err = tx.Commit(); err != nil {
		return memy.PurgeAck{}, err
	}
	return memy.PurgeAck{Sink: s.name, OperationID: batch.OperationID, Epoch: batch.Epoch, Chunk: batch.Chunk}, nil
}
