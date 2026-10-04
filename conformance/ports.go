package conformance

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

// CodecSuite verifies deterministic encoding, typed round trips and invalid input.
// Equal and samples belong to the host; no domain representation is imposed.
func CodecSuite[T any](t *testing.T, codec memy.Codec[T], samples []T, equal func(T, T) bool, malformed [][]byte) {
	t.Helper()
	if codec.Version() == "" || len(samples) == 0 || len(malformed) == 0 {
		t.Fatal("codec fixtures require version, samples and malformed input")
	}
	for _, sample := range samples {
		// Arrange and act: encode independently and decode the canonical result.
		first, err := codec.Encode(sample)
		must(t, err)
		second, err := codec.Encode(sample)
		must(t, err)
		decoded, err := codec.Decode(first)
		must(t, err)
		again, err := codec.Encode(decoded)
		must(t, err)
		// Assert: round trips preserve host values and deterministic byte identity.
		if len(first) == 0 || !bytes.Equal(first, second) || !bytes.Equal(first, again) || !equal(sample, decoded) {
			t.Fatal("codec round trip or determinism failed")
		}
	}
	for _, raw := range malformed {
		if _, err := codec.Decode(raw); err == nil {
			t.Fatalf("malformed input accepted: %q", raw)
		}
	}
}

// AuthorityFixture supplies host identities, an exact grant and outage control.
type AuthorityFixture[A any] struct {
	Adapter memy.Authority[A]
	Allowed A
	Denied  A
	Access  memy.Access
	Actor   string
	Fail    func(error)
}

// AuthoritySuite checks exact decisions, denied identity/scope and explicit outages.
func AuthoritySuite[A any](t *testing.T, factory func(*testing.T) AuthorityFixture[A]) {
	t.Helper()
	// Arrange.
	f := factory(t)
	// Act.
	decision, err := f.Adapter.Check(t.Context(), f.Allowed, f.Access)
	must(t, err)
	denied, denyErr := f.Adapter.Check(t.Context(), f.Denied, f.Access)
	other := f.Access
	other.Scope.Tenant = "foreign"
	foreign, foreignErr := f.Adapter.Check(t.Context(), f.Allowed, other)
	// Assert: only the explicitly granted identity and scope succeed.
	if !decision.Allowed || decision.Actor != f.Actor || decision.Scope != f.Access.Scope ||
		decision.PolicyVersion == "" {
		t.Fatalf("unbound decision: %+v", decision)
	}
	if (denyErr == nil && denied.Allowed) || (foreignErr == nil && foreign.Allowed) {
		t.Fatal("foreign actor or scope authorized")
	}
	failure := errors.New("authority unavailable")
	f.Fail(failure)
	failed, err := f.Adapter.Check(t.Context(), f.Allowed, f.Access)
	if !errors.Is(err, failure) || failed.Allowed {
		t.Fatalf("outage authorized: %+v %v", failed, err)
	}
	f.Fail(nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = f.Adapter.Check(ctx, f.Allowed, f.Access); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

// SourcesFixture provisions typed evidence and exposes host revision changes.
type SourcesFixture[R any] struct {
	Adapter     memy.Sources[R]
	Scope       memy.Scope
	Original    memy.Source[R]
	Replacement memy.Source[R]
	Put         func(memy.Scope, memy.Source[R]) error
	Remove      func(memy.Scope, string)
	Fail        func(error)
}

// SourcesSuite checks exact scoped evidence, revision replacement and outages.
func SourcesSuite[R any](t *testing.T, factory func(*testing.T) SourcesFixture[R]) {
	t.Helper()
	// Arrange: registered evidence is valid only in the fixture scope.
	f := factory(t)
	must(t, f.Put(f.Scope, f.Original))
	// Act and assert: initial success, isolated scope, then invalidated revision.
	must(t, f.Adapter.Validate(t.Context(), f.Scope, f.Original))
	other := f.Scope
	other.Tenant = "foreign"
	if err := f.Adapter.Validate(t.Context(), other, f.Original); err == nil {
		t.Fatal("source crossed scope")
	}
	must(t, f.Put(f.Scope, f.Replacement))
	if err := f.Adapter.Validate(t.Context(), f.Scope, f.Original); err == nil {
		t.Fatal("replaced source revision remained current")
	}
	must(t, f.Adapter.Validate(t.Context(), f.Scope, f.Replacement))
	f.Remove(f.Scope, f.Original.ID)
	if err := f.Adapter.Validate(t.Context(), f.Scope, f.Replacement); err == nil {
		t.Fatal("removed source remained valid")
	}
	must(t, f.Put(f.Scope, f.Replacement))
	failure := errors.New("source unavailable")
	f.Fail(failure)
	if err := f.Adapter.Validate(t.Context(), f.Scope, f.Replacement); !errors.Is(err, failure) {
		t.Fatalf("outage: %v", err)
	}
	f.Fail(nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := f.Adapter.Validate(ctx, f.Scope, f.Replacement); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}

// ClockSuite tests exact injected instants, including deliberate regressions.
// Set belongs to the fixture rather than the production clock interface.
func ClockSuite(t *testing.T, clock memy.Clock, set func(time.Time)) {
	t.Helper()
	for _, instant := range []time.Time{time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 5, 1, 0, 0, 0, 0, time.FixedZone("host", int(time.Hour/time.Second)))} {
		// Arrange and act.
		set(instant)
		got := clock.Now()
		// Assert: clock does not invent ordering or round off the host instant.
		if !got.Equal(instant) {
			t.Fatalf("clock=%v want=%v", got, instant)
		}
	}
}

// CallbackFixture binds a real typed port invocation to host result assertions.
// Fail controls its provider dependency; Invoke must call the adapter itself.
type CallbackFixture struct {
	Invoke func(context.Context) error
	Verify func(*testing.T)
	Fail   func(error)
}

// CallbackSuite is the common cooperative-context/error contract for typed
// retention, extraction, resolution, ranking, projection, merge and reintroduction.
func CallbackSuite(t *testing.T, factory func(*testing.T) CallbackFixture) {
	t.Helper()
	t.Run("typed_success", func(t *testing.T) {
		// Arrange.
		f := factory(t)
		// Act.
		must(t, f.Invoke(t.Context()))
		// Assert: domain-specific output belongs to the host fixture.
		f.Verify(t)
	})
	t.Run("provider_failure", func(t *testing.T) {
		// Arrange.
		f := factory(t)
		if f.Fail == nil {
			t.Skip("deterministic adapter has no injected dependency failure")
		}
		failure := errors.New("provider unavailable")
		f.Fail(failure)
		// Act and assert: errors must remain observable, not become successful zero values.
		if err := f.Invoke(t.Context()); !errors.Is(err, failure) {
			t.Fatalf("provider failure: %v", err)
		}
	})
	t.Run("cancellation", func(t *testing.T) {
		// Arrange.
		f := factory(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		// Act and assert.
		if err := f.Invoke(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation: %v", err)
		}
	})
}
