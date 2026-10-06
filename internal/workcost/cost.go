// Package workcost provides opt-in private instrumentation for structural cost
// tests and benchmarks. It is not an Engine port or a public observability API.
package workcost

import "sync/atomic"

type Counters struct {
	DecodedDocs       atomic.Int64
	DecodedBytes      atomic.Int64
	MemoryCopiedBytes atomic.Int64
	IndexNodes        atomic.Int64
	SQLMetadataRows   atomic.Int64
	SQLValueRows      atomic.Int64
	SQLValueBytes     atomic.Int64
}

// The private benchmark observer must aggregate concurrent adapter work in one process.
//
//nolint:gochecknoglobals // Process-wide opt-in instrumentation; Start/Stop serialize ownership.
var active atomic.Pointer[Counters]

// Start installs process-wide measurement. Callers must serialize sessions and
// Stop before destroying the fixture. Counters permit concurrent adapter work.
func Start(c *Counters) {
	if !active.CompareAndSwap(nil, c) {
		panic("workcost: overlapping session")
	}
}
func Stop(c *Counters) {
	if !active.CompareAndSwap(c, nil) {
		panic("workcost: wrong session")
	}
}
func Decode(n int) {
	if c := active.Load(); c != nil {
		c.DecodedDocs.Add(1)
		c.DecodedBytes.Add(int64(n))
	}
}
func Copy(n int) {
	if c := active.Load(); c != nil {
		c.MemoryCopiedBytes.Add(int64(n))
	}
}
func Node() {
	if c := active.Load(); c != nil {
		c.IndexNodes.Add(1)
	}
}
func Metadata() {
	if c := active.Load(); c != nil {
		c.SQLMetadataRows.Add(1)
	}
}
func Value(n int) {
	if c := active.Load(); c != nil {
		c.SQLValueRows.Add(1)
		c.SQLValueBytes.Add(int64(n))
	}
}
