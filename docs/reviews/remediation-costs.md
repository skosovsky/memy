# Local remediation cost evidence — 2026-10-06

Saved copy of observed terminal output from root exec session36735. This is not
a rerun or a production benchmark. Setup is outside the measured loop; packing
uses one trial per size and an explicit five-second context censor.

```sh
GOCACHE=/tmp/memy-go-build go test -run '^$' -bench BenchmarkJSONPackingFullBody -benchtime=1x -benchmem ./reference
GOCACHE=/tmp/memy-go-build go test -run '^$' -bench BenchmarkRejectedPutAdmission -benchtime=100x -benchmem ./internal/kv
```

```text
goos: darwin
goarch: arm64
pkg: github.com/skosovsky/memy/reference
cpu: Apple M1 Max
BenchmarkJSONPackingFullBody/100-10         1       7876791 ns/op         0 censored/op     6977040 B/op     1357 allocs/op
BenchmarkJSONPackingFullBody/1000-10        1     792838125 ns/op         0 censored/op   829380392 B/op    14581 allocs/op
BenchmarkJSONPackingFullBody/10000-10       1    5000436958 ns/op         1.000 censored/op 9210358384 B/op 67085 allocs/op
PASS
ok  github.com/skosovsky/memy/reference 6.354s

goos: darwin
goarch: arm64
pkg: github.com/skosovsky/memy/internal/kv
cpu: Apple M1 Max
BenchmarkRejectedPutAdmission-10         100        14.58 ns/op         0 B/op         0 allocs/op
PASS
ok  github.com/skosovsky/memy/internal/kv 0.601s
```

The 10,000-projection trial did not finish selection; its runtime is censored.
Allocated bytes are totals, not peak resident memory. Figures are single local
trials rather than distributions or cross-platform guarantees.
