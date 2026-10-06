# Task03 validation

The production/tests/contracts snapshot is recorded in `source-hashes.json`.
`final-checks.json` records vet, all three runnable examples, formatting and
diff-check results. `final-race.txt` is the final full-suite race output.

Command: `GOCACHE=/private/tmp/memy-task03-go-cache go test -race -count=1 -timeout=20m ./...`.
Full-suite outcome: PASS, exit0; root package 682.555s. The suite includes the existing 10,000-node graph
regression without reducing its input or assertions.

Two earlier full race invocations were interrupted with SIGTERM (exit143) after
review findings: cancellation/availability/state handling, then overwritten raw
signal scores. Their unfinished outputs are `pre-review-race-interrupted.txt` and
`pre-signal-fix-race-interrupted.txt`; neither is passing evidence. The final
invocation includes all production fixes and their regression tests, including
raw-score preservation with duplicate/permutation coverage.

Independent targeted race checks cover candidate metadata/bounds before canonical
decode, projection policy isolation and invalid selections, cost limits,
source/authority/expiry/revocation/cancellation, concurrent Forget outside held
callbacks, and still-readable Active-to-Conflicted state transitions on both
memory and SQLite. Review reports list their own commands and findings.

No retrieval-quality or external-backend billing conclusion is inferred from the
offline examples. Task02 benchmark evidence is unchanged. Existing user edits to
Makefile and scripts/release.sh are excluded from this task.
