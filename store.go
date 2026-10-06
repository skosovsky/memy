package memy

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"unicode/utf8"
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
		if !validIdentifier(part) {
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

// SchemaVersion is the only supported persisted envelope and database format.
const SchemaVersion uint32 = 3

// validIdentifier compares exact text; it never normalizes identity.
func validIdentifier(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 1024 &&
		!strings.ContainsRune(value, '\x00')
}

func validPurpose(value string) bool { return value == "" || validIdentifier(value) }

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
	Scan(ScanOptions) (ScanPage, error)
	Put(key string, expected Version, data []byte) (Version, error)
	Delete(key string, expected Version) (Version, error)
}

// StoreCapabilities declares executable guarantees, not optimistic defaults.
type StoreCapabilities struct {
	Atomic           bool
	ConditionalWrite bool
	FencedView       bool
	Durable          bool
	SchemaVersion    uint32
}

// Store provides consistent reads, scoped atomic updates and explicit scoped
// exclusion. FencedView excludes Update in its scope until callback completion;
// independent scopes do not wait for that read callback. View need not exclude
// mutation. Callback errors and panics roll back;
// panics propagate. Cancellation observed before commit rolls back. A failure
// after durable commit can return ErrUnknownOutcome. Callbacks must not recurse
// into this store. Direct use is privileged: authority is enforced by Engine.
type Store interface {
	Capabilities() StoreCapabilities
	View(context.Context, Scope, func(Bucket) error) error
	FencedView(context.Context, Scope, func(Bucket) error) error
	Update(context.Context, Scope, func(Bucket) error) error
	Close() error
}
