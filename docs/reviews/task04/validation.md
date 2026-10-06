# Task04 validation

`source-hashes.json` pins the implementation, tests, module files, current
contracts, executable schemas, manifest and saved golden report. Both independent
reviewers verified these hashes and independently reproduced the compiled CLI's
normal report byte for byte.

After acceptance, only status text in docs/acceptance.md and docs/quality.md
changed. The hash manifest records this documentation delta explicitly; Go
sources, schemas, module files, corpus and golden report retain their tested
hashes. Both reviewers rechecked the final documentation and current hashes.

`targeted-race.txt` records the final quality/CLI/integration race gate: pass,
exit 0; quality 129.795s and CLI 36.361s. The times are local diagnostics under
concurrent test execution, not a performance result.

The full repository command is
`GOCACHE=/private/tmp/memy-task03-go-cache go test -race -count=1 -timeout=20m ./...`.
Final outcome: pass, exit 0; 915.557s for the root package. `race.txt` and
`final-race-result.json` record the successful whole-suite result. The existing 10,000-node graph regression
is included without reducing its input or assertions.

`final-checks.json` records successful vet, formatting, diff validation and the
lifecycle, retrieval and consumer-port integration examples. The normal quality
command is covered separately by `cli-validation.json`: 14 real compiled-process
cases verify exits 0/1/2, report persistence before exit, mode 0600, low-budget
handling, safe malformed input and a byte-identical normal rerun.

The golden report has 16 registered plans with two repeats: 32 passing parent
protocol verdicts and 14 negative evaluator probes (10 failed, four unknown).
Expected rejection and permissive-host boundary outcomes keep their failed
descriptive quality evidence. Scripted fixtures do not establish real model
quality, general poisoning resistance, provider billing or performance savings.

Earlier targeted runs during implementation and review were not used as final
passing evidence. Confirmed findings and their independent regression checks
are recorded in `correctness.md`. Task02 performance evidence and Task03 review
snapshots are preserved as historical artifacts.

Existing user edits to Makefile and scripts/release.sh are excluded from Task04.
