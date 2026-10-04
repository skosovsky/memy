package memy

import (
	"context"
	"encoding/json"
	"math"
	"strings"
)

// Scope is assigned by the authenticated host, never by payload content.
type Scope struct {
	Tenant    string `json:"tenant"`
	Namespace string `json:"namespace"`
	Subject   string `json:"subject"`
}

// Validate rejects incomplete scopes and oversized identifiers.
func (s Scope) Validate() error {
	for _, part := range []string{s.Tenant, s.Namespace, s.Subject} {
		if strings.TrimSpace(part) == "" || len(part) > 1024 || strings.ContainsRune(part, '\x00') {
			return ErrInvalid
		}
	}
	return nil
}

// Key uses structural encoding to avoid tenant/namespace delimiter collisions.
func (s Scope) Key() string {
	encoded, _ := json.Marshal(s)
	return string(encoded)
}

// Version is a per-key CAS version. Zero denotes a never-written key.
// Deletion retains its version and subsequent reintroduction increments it.
type Version uint64

// MaxVersion is the portable maximum supported by reference stores.
const MaxVersion Version = math.MaxInt64

// Value holds detached bytes. A nil Data with nonzero Version is a tombstone.
type Value struct {
	Version Version
	Data    []byte
}

// Entry is a live scoped key/value pair.
type Entry struct {
	Key   string
	Value Value
}

// Bucket is a synchronous transaction-local view. It cannot be used after its
// callback or concurrently. Read-only buckets reject Put/Delete. Returned data
// is detached; retaining or modifying it does not change stored values.
type Bucket interface {
	Get(key string) (Value, error)
	List(prefix string) ([]Entry, error)
	Put(key string, expected Version, data []byte) (Version, error)
	Delete(key string, expected Version) (Version, error)
}

// StoreCapabilities declares executable guarantees, not optimistic defaults.
type StoreCapabilities struct {
	Atomic           bool
	ConditionalWrite bool
	Durable          bool
	SchemaVersion    uint32
}

// Store serializes scoped atomic updates. Callback errors and panics roll back;
// panics propagate. Cancellation observed before commit rolls back. A failure
// after durable commit can return ErrUnknownOutcome. Callbacks must not recurse
// into this store. Direct use is privileged: authority is enforced by Engine.
type Store interface {
	Capabilities() StoreCapabilities
	View(context.Context, Scope, func(Bucket) error) error
	Update(context.Context, Scope, func(Bucket) error) error
	Close() error
}
