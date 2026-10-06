package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/memory"
)

func TestSlowScopedCallbackAllowsIndependentScope(t *testing.T) {
	// Arrange: scope A callback is synchronously held at a known boundary.
	s := memory.New()
	t.Cleanup(func() { _ = s.Close() })
	a := memy.Scope{Tenant: "tenant", Namespace: "ns", Subject: "a"}
	b := a
	b.Subject = "b"
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.View(context.Background(), a, func(memy.Bucket) error {
			close(entered)
			<-release
			return nil
		})
	}()
	<-entered
	defer func() {
		close(release)
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()

	// Act: an independent scope mutation runs while A is still paused.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.Update(ctx, b, func(bucket memy.Bucket) error {
		_, err := bucket.Put("key", 0, []byte("value"))
		return err
	})

	// Assert: scope B did not require A's callback to finish.
	if err != nil {
		t.Fatal(err)
	}
}
