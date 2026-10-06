# Independent correctness review — 2026-10-06

Status: **ACCEPT** for stage 10 and the final reviewed F01–F10/D01–D70 scope.
There is no open confirmed error within this scope. This is not an
absence-of-unknown-bugs claim. This report was prepared independently, without consulting the final
completeness review.

## Scope and method

Read-only review of task2 F01–F10, normative remediation contracts, D01–D70,
source diffs and migration/recovery documentation. Stages were reviewed separately
before their commits. Targeted fresh race tests, adversarial temporary programs
and an overlay probe supplemented inspection. Only this review report was
written by the reviewer. No production file or other review was modified.

## Findings resolved before acceptance

- The initial codec contract could not certify arbitrary custom callback
  determinism by finite checks. The final contract makes deterministic callbacks
  a host admission obligation and documents the limit.
- Release fixtures inherited global Git signing configuration and could hang.
  Their isolated Git environment and explicit lightweight production tag now
  make the tested publication behavior reproducible.
- Unicode inspection skipped named byte slices with custom text marshalers and
  could panic on nil pointer text-marshaler map keys. Final tests and independent
  probes verify emitted custom strings are checked and nil keys follow stdlib
  behavior.
- Final purge reauthorization could erase already committed acknowledgements and
  charged work. The final continuation returns its latest receipt and confirmed
  callback count on errors. An independent authority-outage overlay verified
  preservation after both sink acknowledgements had committed.
- The SQLite application-table classification used SQL LIKE's wildcard underscore.
  A legitimate `sqliteXhost` table was mistaken for an internal table and allowed
  bootstrap. Literal-prefix GLOB and both foreign-table regressions now reject it
  with ErrSchema without creating memy tables.

Each finding was re-reviewed after correction. The conformance stale-old-search
acknowledgement case was also reviewed after the separate completeness reviewer
requested permanent coverage; the final upgraded visible revision remains exact.
The final historical quality attribution was independently checked after its
correction: all 127 frozen source-manifest hashes match e68eaf3, while 89 differ
at e12c8fc. The historical protocol validation and Oct 6 defect reproductions
therefore retain their distinct source identities.

## Independent evidence

| Area | Reviewed behavior and focused evidence |
|---|---|
| Release | Source checkout/index/refs unchanged; exact tag only; attached/detached, retry and unknown outcomes; 10 local fixtures passed (13.670s), shell syntax passed. |
| Codec | Generic json.Number precision, strict Unicode, custom marshalers, cycles, detached ownership and json/v2 migration; fresh codec race tests passed (1.726s), two temporary regressions rechecked. |
| ExactDedup | Expiry derives from actual exact revision lineage; unrelated singleton and unlimited lineage preserve correct expiry; Memory/SQLite and internal stamper races passed (2.897s). |
| Cursor | Length-framed exact binding, binary arbitrary keys, canonical base64, HMAC/generation, old format and 5563-byte bound; independent KV/Memory/SQLite races passed (1.661s/1.242s/2.645s). |
| Projection | Full effective BYOT snapshot before callbacks and final fenced comparison, current and historical reads, cache projection/v2, selected and omitted refs; focused races passed (6.811s). |
| Sweep | Confirmed per-transaction delta, rollback/unknown exclusion, pending and final acknowledgement preservation, original identity recovery; focused races passed (2.878s), authority-outage overlay passed. |
| Quality | Delivered context budgets, actual grader invocation evidence, validation-before-setup, fail versus unknown, safe malformed metrics, conformance isolation/revision/chunk/replay boundaries; targeted races passed (9.175s/8.257s), full quality race passed (65.334s). Independent AST comparison retained all 105 functions (98 unchanged, 7 intended edits); recursively expanded schema semantics were equivalent. |
| Storage/API | Active maintenance discriminator, response BudgetCharged rename without durable identity change, rejected-memory-admission ownership/allocations, SQLite schema/table/layout checks, post-close minimum; final SQLite race passed (14.674s), KV/Memory/reference races passed (5.342s/1.728s/1.598s), focused root races passed (39.743s). |
| Final docs/example | Fresh lifecycle execution completed the original failed Forget through Sweep at epoch 1. Fresh pending/final acknowledgement races passed (1.756s). Regenerated saved report validated against executable quality-report-v2 JSON Schema; all 32 scenario final protocol verdicts are pass. Final diff whitespace check passed. |

Earlier-stage timings identify separate local invocations; they are not aggregate
benchmark or efficacy measurements. Baseline runtime evidence is historical
compiled behavior at e12c8fc, not a new-API compilation failure. The recorded
packing benchmark's 10,000-candidate result is censored, not completed throughput
or peak RSS. Migration explicitly covers json/v2, projection/v2, cursor/v2,
BudgetCharged and old consolidation digest conflicts while storage remains v3.

README, current design, API recovery, migration and maintenance describe the same
implemented boundaries. Historical dates/SHA and conditional feature analysis
remain identified separately. The external ai-libs feature specification covers
MEM-001–MEM-006 and is outside the memy commit. The final source manifest was
independently checked: all 189 file byte counts and hashes match, no admitted
tracked source is missing, and compact sorted-key JSON yields aggregate
`c31215356383f16bfa6854cff615cdf1736c1f9efa5b2fefc4d637b679aa97ca`.
The external feature file also matches its recorded 31,677 bytes and exact hash. The example's maintenance identity
and original purge identity are distinct; receipts are consumed alongside errors.

## Final gate and limits

The parent communicated the authoritative terminal tool result: fresh main
`make validate` returned exit 0. Independently inspected saved output is an exact
byte copy of the raw terminal log: 394,210 bytes, 11,548 lines. Format/vet passed,
lint reported 0 issues, all-package fresh race tests passed (root 590.823s,
internal quality 75.869s, SQLite 3.493s, KV 2.289s). All four examples completed
and ten local release fixtures passed in 16.006s. Fuzz seed execution is covered
by the suite; this does not certify a long fuzz campaign. The final source
manifest still matches after terminal evidence recording. No pending execution
is counted as passed. The final report accurately reflects these results.

Review and execution are local Go 1.27.1 darwin/arm64 with CGO, temporary SQLite,
scripted providers and specified adversarial cases. No live model, remote release,
Linux execution, production quality, hostile-database authenticity, forensic
erasure, long-duration fuzz campaign or universal custom-codec correctness is
certified. Structural schema checks do not authenticate history. Unknown commit
outcomes remain unknown; lost-response retries do not promise exactly-once
reported counters. Host callback determinism, downstream cache authorization,
remote backend costs and cooperative cancellation retain their documented host
obligations.
