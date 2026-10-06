package reference_test

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/reference"
)

// These synthetic full-body trials measure the offline reference policy, not
// retrieval quality. Five-second censoring prevents an unbounded large trial.
func BenchmarkJSONPackingFullBody(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			body := memy.ProjectedRecallResult[string, string]{Projections: make([]memy.Projection[string, string], n)}
			for i := range body.Projections {
				body.Projections[i] = memy.Projection[string, string]{
					RecordID: fmt.Sprintf("record-%d", i),
					Revision: 1,
					Output:   strings.Repeat("x", 100),
					Trust:    "data",
				}
			}
			policy := reference.JSONPacking[string, string]{}
			censored := 0
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				ctx, cancel := context.WithTimeout(b.Context(), 5*time.Second)
				_, err := policy.Select(ctx, body, 1<<30)
				cancel()
				if errors.Is(err, context.DeadlineExceeded) {
					censored++
				} else if err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(censored)/float64(b.N), "censored/op")
		})
	}
}
