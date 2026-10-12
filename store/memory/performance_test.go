package memory

import (
	"context"
	"strconv"
	"testing"

	"github.com/skosovsky/memy"
)

// BenchmarkPerformanceAddressedGet matches the original audit's negative
// control: one 1-KiB addressed read with increasing unrelated keys.
func BenchmarkPerformanceAddressedGet(b *testing.B) {
	for _, n := range []int{1, 1_000, 10_000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			s := New()
			defer s.Close()
			ctx := context.Background()
			scope := memy.Scope{Tenant: "t", Namespace: "n", Subject: "s"}
			populateAddressedGetFixture(b, s, scope, n)
			fn := func(bucket memy.Bucket) error {
				value, err := bucket.Get("0")
				if len(value.Data) != 1024 {
					b.Fatal("bad data")
				}
				return err
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				if err := s.View(ctx, scope, fn); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func populateAddressedGetFixture(b *testing.B, s *Store, scope memy.Scope, n int) {
	b.Helper()
	payload := make([]byte, 1024)
	if err := s.Update(context.Background(), scope, func(bucket memy.Bucket) error {
		for j := range n {
			if _, err := bucket.Put(strconv.Itoa(j), 0, payload); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		b.Fatal(err)
	}
}
