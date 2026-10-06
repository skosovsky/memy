package kv

import (
	"context"
	"errors"
	"testing"

	"github.com/skosovsky/memy"
)

func TestRejectedPutDoesNotClonePayload(t *testing.T) {
	for _, mode := range []string{"key", "cancelled", "readonly", "cas", "overflow"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange: every rejection happens before caller bytes need detached ownership.
			var index Index
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			bucket := New(ctx, map[string]memy.Value{}, &index, Binding{}, true)
			payload := make([]byte, 1<<20)
			key, expected, want := "record", memy.Version(0), memy.ErrInvalid
			switch mode {
			case "key":
				key = ""
			case "cancelled":
				cancel()
				want = context.Canceled
			case "readonly":
				bucket.writable = false
				want = memy.ErrUnsupported
			case "cas":
				expected = 1
				want = memy.ErrConflict
			case "overflow":
				bucket.Binding.Generation = memy.MaxVersion
				want = memy.ErrConflict
			}
			// Act.
			allocations := testing.AllocsPerRun(100, func() {
				_, err := bucket.Put(key, expected, payload)
				if !errors.Is(err, want) {
					t.Fatalf("err=%v", err)
				}
			})
			// Assert: neither a payload copy nor a transaction write was admitted.
			if allocations != 0 || len(bucket.pending) != 0 {
				t.Fatalf("allocs=%f pending=%v", allocations, bucket.pending)
			}
		})
	}
}

func BenchmarkRejectedPutAdmission(b *testing.B) {
	var index Index
	bucket := New(b.Context(), map[string]memy.Value{}, &index, Binding{}, true)
	payload := make([]byte, 1<<20)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, err := bucket.Put("", 0, payload)
		if !errors.Is(err, memy.ErrInvalid) {
			b.Fatal(err)
		}
	}
}
