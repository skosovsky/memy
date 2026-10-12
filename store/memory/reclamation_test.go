package memory

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/skosovsky/memy"
)

func freshScope(subject string) memy.Scope {
	return memy.Scope{Tenant: "tenant", Namespace: "namespace", Subject: subject}
}

func awaitScopeUsers(t *testing.T, s *Store, scope memy.Scope, count int) *scopeState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		state := s.scopes[scope.Key()]
		matched := state != nil && state.users == count
		s.mu.Unlock()
		if matched {
			return state
		}
		runtime.Gosched()
	}
	t.Fatalf("scope did not reach %d registered users", count)
	return nil
}

func assertScopeCount(t *testing.T, s *Store, count int) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.scopes) != count {
		t.Fatalf("scope registry has %d entries, want %d", len(s.scopes), count)
	}
}

func TestReadonlyMissReclaimsNeverWrittenScopes(t *testing.T) {
	// Arrange: each access method opens a previously unknown exact scope.
	s := New()
	t.Cleanup(func() { _ = s.Close() })
	methods := []func(context.Context, memy.Scope, func(memy.Bucket) error) error{s.View, s.FencedView, s.Update}

	// Act: 10,000 completed misses do not commit any writes.
	for i := range 10_000 {
		err := methods[i%len(methods)](context.Background(), freshScope(strconv.Itoa(i)), func(b memy.Bucket) error {
			value, err := b.Get("missing")
			if value.Version != 0 || len(value.Data) != 0 {
				t.Fatal("fresh scope was not empty")
			}
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	// Assert: no inactive never-written scope remains reachable from the store.
	assertScopeCount(t, s, 0)
}

func TestFreshScopeWaitersShareGateAndPreserveCommit(t *testing.T) {
	// Arrange: hold the first callback while a fenced reader and writer queue.
	s := New()
	t.Cleanup(func() { _ = s.Close() })
	scope := freshScope("shared")
	entered, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- s.View(context.Background(), scope, func(memy.Bucket) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	state := awaitScopeUsers(t, s, scope, 1)
	fenced, updated := make(chan error, 1), make(chan error, 1)
	go func() { fenced <- s.FencedView(context.Background(), scope, func(memy.Bucket) error { return nil }) }()
	go func() {
		updated <- s.Update(context.Background(), scope, func(b memy.Bucket) error {
			_, err := b.Put("key", 0, []byte("committed"))
			return err
		})
	}()
	if waiting := awaitScopeUsers(t, s, scope, 3); waiting != state {
		t.Fatal("waiters registered a separate gate")
	}

	// Act: let all three operations complete, then open another reader.
	close(release)
	for _, result := range []chan error{first, fenced, updated} {
		if err := <-result; err != nil {
			t.Fatal(err)
		}
	}
	err := s.View(context.Background(), scope, func(b memy.Bucket) error {
		value, err := b.Get("key")
		if value.Version != 1 || string(value.Data) != "committed" {
			t.Fatalf("lost committed value: %+v", value)
		}
		return err
	})

	// Assert: committed state and its original exclusion gate survived.
	if err != nil {
		t.Fatal(err)
	}
	assertScopeCount(t, s, 1)
	if remaining := awaitScopeUsers(t, s, scope, 0); remaining != state || remaining.generation != 1 {
		t.Fatal("committed state or generation changed")
	}
}

func TestWaitingCancellationReleasesScopeRegistration(t *testing.T) {
	// Arrange: an empty callback holds the gate while another operation waits.
	s := New()
	t.Cleanup(func() { _ = s.Close() })
	scope := freshScope("cancel")
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.View(context.Background(), scope, func(memy.Bucket) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() {
		waiting <- s.Update(ctx, scope, func(memy.Bucket) error {
			t.Error("cancelled waiting callback ran")
			return nil
		})
	}()
	awaitScopeUsers(t, s, scope, 2)

	// Act: cancel the waiter before releasing the empty callback.
	cancel()
	err := <-waiting
	awaitScopeUsers(t, s, scope, 1)
	close(release)
	if completionErr := <-done; completionErr != nil {
		t.Fatal(completionErr)
	}

	// Assert: cancellation detached the waiter and the last callback reclaimed state.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting operation: %v", err)
	}
	assertScopeCount(t, s, 0)
}

func TestFailedOrPanickingWritesReclaimFreshScope(t *testing.T) {
	for _, mode := range []string{"error", "cancel", "panic"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange: prepare a write which must never commit.
			s := New()
			t.Cleanup(func() { _ = s.Close() })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			failure := errors.New("callback failed")
			var err error
			var recovered any

			// Act: leave the callback through each supported rollback path.
			func() {
				defer func() { recovered = recover() }()
				err = s.Update(ctx, freshScope(mode), func(b memy.Bucket) error {
					if _, putErr := b.Put("key", 0, []byte("uncommitted")); putErr != nil {
						return putErr
					}
					switch mode {
					case "error":
						return failure
					case "cancel":
						cancel()
					case "panic":
						panic("callback panic")
					}
					return nil
				})
			}()

			// Assert: rollback leaves neither data nor an unused scope gate.
			if mode == "error" && !errors.Is(err, failure) || mode == "cancel" && !errors.Is(err, context.Canceled) ||
				mode == "panic" && recovered != "callback panic" {
				t.Fatalf("unexpected completion: err=%v panic=%v", err, recovered)
			}
			assertScopeCount(t, s, 0)
		})
	}
}

func TestTombstoneScopeRetainsVersionAndGeneration(t *testing.T) {
	// Arrange: deleting an absent key is a committed CAS mutation.
	s := New()
	t.Cleanup(func() { _ = s.Close() })
	scope := freshScope("tombstone")
	err := s.Update(context.Background(), scope, func(b memy.Bucket) error {
		_, err := b.Delete("key", 0)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}

	// Act: read the tombstone and attempt a stale initial-version write.
	err = s.Update(context.Background(), scope, func(b memy.Bucket) error {
		value, getErr := b.Get("key")
		if getErr != nil || value.Version != 1 || len(value.Data) != 0 {
			t.Fatalf("tombstone: %+v, %v", value, getErr)
		}
		_, putErr := b.Put("key", 0, []byte("stale"))
		return putErr
	})

	// Assert: readonly/failed accesses do not reset CAS history or cursor binding.
	if !errors.Is(err, memy.ErrConflict) {
		t.Fatalf("stale CAS: %v", err)
	}
	assertScopeCount(t, s, 1)
	if state := awaitScopeUsers(t, s, scope, 0); state.generation != 1 {
		t.Fatalf("generation reset: %d", state.generation)
	}
}

func TestCloseWithFreshScopeCallbackAndWaiters(t *testing.T) {
	// Arrange: one live callback, a cancellable waiter and a surviving waiter.
	s := New()
	scope := freshScope("close")
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.View(context.Background(), scope, func(memy.Bucket) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting, cancelled, closed := make(chan error, 1), make(chan error, 1), make(chan error, 1)
	callback := func(memy.Bucket) error { t.Error("waiting callback ran after Close"); return memy.ErrInvalid }
	go func() { waiting <- s.FencedView(context.Background(), scope, callback) }()
	go func() { cancelled <- s.Update(ctx, scope, callback) }()
	state := awaitScopeUsers(t, s, scope, 3)

	// Act: Close prevents callback admission, cancellation drops one waiter.
	go func() { closed <- s.Close() }()
	deadline := time.Now().Add(5 * time.Second)
	for !s.closed.Load() && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if !s.closed.Load() {
		t.Fatal("Close did not prevent admission")
	}
	cancel()
	if err := <-cancelled; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter: %v", err)
	}
	select {
	case <-closed:
		t.Fatal("Close returned before the active callback completed")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := <-waiting; !errors.Is(err, memy.ErrClosed) {
		t.Fatalf("surviving waiter: %v", err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}

	// Assert: all registrations finish and Close removes store references.
	assertScopeCount(t, s, 0)
	s.mu.Lock()
	users := state.users
	s.mu.Unlock()
	if users != 0 || state.values != nil {
		t.Fatalf("Close retained state: users=%d values=%v", users, state.values)
	}
}
