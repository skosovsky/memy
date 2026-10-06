# Task02 implementation progress

This is an implementation log, not independent acceptance. Task02 is incomplete
and must not be committed before both final reviewers accept the full task.
Task01 is committed as `9f7b50f` with the configured SSH signature.

Implemented so far:

- Exact Consolidate input loading preserves separate read authorization,
  canonical eligibility, current evidence/retention/lineage checks, input budget
  and final deadline gate. No full Snapshot precedes fixed-input preparation.
- Historical source/reverse-lineage membership indexes replace repeated scope
  history scans for selection. Commit updates indexes atomically; traversal
  validates hits against exact canonical revisions and visits each ID once.
- Memory has per-scope exclusion and an addressed transaction overlay. Only
  successful touched keys reach canonical storage. A live-key AVL range index
  prevents prefix reads from iterating unrelated values/tombstones.
- SQLite uses transaction-local SQL Get/Put/Delete and indexed prefix ranges.
  The old full-scope load/clone/persist implementation is removed. Update uses BEGIN IMMEDIATE; View/FencedView use a separate WAL read pool.
  Explicit scope file locks fence independent handles and processes without a
  remote managed-write callback retaining SQLite's global writer reservation.

New regression evidence:

- Shared conformance covers bounded pages, byte budgets, cursor scope/prefix/
  generation, rollback and escaped scan lifetime on both adapters.
- TestScopeFencingAcrossProcesses verifies actual subprocess exclusion, scope B
  writes during a held scope A fence, and kernel cleanup after process death.
- TestCursorSurvivesHandleReopenButNotMutationOrDatabaseCopy checks reopen,
  generation invalidation and physical database identity binding.
- TestCallbacksAllowMutationAndRejectRevokedFinalDelivery checks that Recall/
  Project do not delay revoke and reject outputs after callback-time revocation.
- TestIndexSnapshotSurvivesRotationsAndRemovals checks overlay root isolation.

- TestConsolidationReadsOnlySelectedEvidence on both adapters verifies unrelated
  source removal does not veto independent exact inputs and no history listing.
- TestDependentSelectionTraversesHistoricalChainBranchesAndCycles verifies a
  reverse ordered 10k chain, branches, historical membership and a cycle.
- TestSlowScopedCallbackAllowsIndependentScope verifies memory scope isolation.
- TestIndexMaintainsOrderedRangesThroughMutation uses a deterministic map oracle
  and checks AVL ordering/heights/balance after repeated insertion and deletion.
- Existing shared adapter conformance and full tests with race pass for the
  addressed storage stage. go vet passes. Full race tests, vet and both offline examples pass at the current stage.
  Fresh final checks are still required.

Completed storage API work replaces List with authenticated bounded Scan pages
(entry/byte limits and explicit stale cursor); schema 3 stores cursor secret and
per-scope generations. Path-copying AVL overlays preserve rollback without
copying unrelated state. Rank/Project callbacks now run outside transactions
with fresh fenced canonical revalidation before final delivery.

Snapshot now returns bounded SnapshotPage results with authenticated read-plan
continuation. Empty eligible pages still advance; actor/policy/read-option
changes reject continuation. Offline quality/test consumers explicitly collect
pages as host code.

Forget now installs a durable active-purge scope fence before enumeration and
continues through bounded seed/expand/history/membership/proposal cleanup.
CanonicalComplete precedes sink chunk delivery; final Complete requires every
chunk acknowledgement. The semantic request identity excludes transport budgets.
A native Limit=1 regression covers memory and SQLite interruption/reopen, bounded
scan/output, hidden payload during partial cleanup, independent-scope writes,
stale derived jobs and idempotent replay. Corruption regressions reject foreign
or missing active fences and invalid durable progress without deleting history.
Sink conformance checks the exact chunk acknowledgement in addition to epoch.

Sweep now uses a durable host-driven pass with explicit Limit/MaxBytes, an
active maintenance fence and current retention evaluation for every visited
revision. It resumes bounded purge chunks and proposal-history cleanup after
reopen. Native one-entry tests cover memory and SQLite, canonical absence while
active, exact erased records/draft count and enumeration-free completed replay.
Legacy full selection/revoke/proposal-cleanup helpers have been removed.
The old synthetic traversal oracle is test-local only; it is not proof that the
new production pipeline handles the full 10k branching/historical corpus.

Outstanding scope includes final Sweep retry/concurrency/fault coverage,
all specified fault/race/cursor/reopen regressions, complete performance corpus
(including growing lineage/cold/contention/private work instrumentation), docs,
schemas as needed, examples and two final independent reviewers.

Baseline process and measurements are described in baseline-method.md. Initial
corpus samples run against the compiled pre-change production binary. The complete raw output is preserved in baseline-raw.txt, along with the original
harness source.
Interim post-addressing samples are diagnostic only: one warm repetition, while
the legacy baseline may compete for workstation resources. They must not be
promoted to the required final performance report.

Do not include preexisting Makefile edits or unrelated iCloud duplicate files
(`Makefile 2`, `.gitignore 2`) in the task02 commit. No push/release.

A SourceForget smoke measurement at 1k records succeeds on both adapters and
asserts complete plus exactly 1k batch records. The current corpus clones the
new membership indexes as well as canonical revisions; the historical baseline
continues to describe its own pre-index schema. Latest stage evidence is in
/tmp/memy-task02-snapshot-race2.txt, /tmp/memy-task02-source-interim.txt,
/tmp/memy-task02-lifecycle.log and /tmp/memy-task02-quality.json.

Private instrumentation and a 1k/10k structural cost regression are now present.
Get and fixed-input Consolidate preserve decoded documents/bytes and exact value
copy/read counts as unrelated scope grows, on both adapters. A complete 30-case
warm corpus with three repetitions is running; raw output is currently external
at /tmp/memy-task02-final-warm-raw.txt and must be copied only after terminal state.
Growing fan-in lineage, SQLite handle-cold and managed callback contention harnesses
compile but have not yet supplied accepted measurement evidence. Native production
10k historical/branch purge coverage remains required. No final task02 acceptance.

TestNativePurgeTraversesLongHistoricalGraph now drives the real bounded pipeline
on memory and SQLite: a 10k chain with branch/cycle membership, a current head
whose old revision alone retains the dependent edge, and two unrelated heads.
It asserts all 10003 selected IDs are uniquely emitted, every selected head has
exactly one tombstone revision, only two unrelated history entries survive, and
completed replay creates no extra chunk. The non-race run passed; final race
coverage is still required. The synthetic traversal test is not used as its proof.

The obsolete synthetic-only graph traversal helper/test has been deleted after
its guarantees were replaced by TestNativePurgeTraversesLongHistoricalGraph.
Invalid sink Chunk acknowledgement now has an explicit failed/reopen/repair
regression in TestFailedPurgeRemainsFencedAndCanRecoverAcrossReopen. It passed.

First independent reviewers are auditing task02; acceptance is not achieved.
Extended growing-lineage/reopen run is terminal and preserved in extended-raw.txt
and extended-summary.json. Callback contention is running with 20 iterations ×
three repetitions per adapter. Completeness identified remaining requirements:
high fan-in traversal must avoid repeated full child-revision decoding across
bounded calls; independently opened Engine idempotency needs a synchronized test;
historical lineage/contention/reopen baseline measurements still need completion.
Correctness confirmed Sweep authority-rollout and false-done/live-fence defects.
Both have production fixes plus reproductions/regressions; independent closure
and final full checks remain required. Current performance artifacts are stage
measurements, not certification of the later corrected source snapshot.

R3 now uses inclusion proofs and durable canonical-root evidence per exact
revision, eliminating repeated full child decode across batches. Native Limit1
fan-in regression (100/200/400 parents) passed on both adapters: 2.08/4.18/8.44 MB
decoded, approximately proportional graph growth. Shape/proof unit tests reject
foreign revisions, counts and extra siblings. Fixture builders now emit the new
proof schema. R4 checks Sweep's exact canonical revision key; R5 validates purge
phase/receipt cleanup agreement and rejects premature emit/done. Fresh targeted
schema/continuation tests and vet pass. Full race is running, including graph10k;
reviewer is checking closure and proof invariants. Earlier performance outputs
remain historical stage evidence and need final snapshot updates as appropriate.

Cross-handle lifecycle coverage now starts identical Commit and bounded Forget
concurrently from two independent SQLite Store handles and Engine instances. The
race run passed; exact head revision assertion is added for the final rerun.
First-round acceptance remains 60%, with confirmed correctness findings undergoing
independent closure. The new purge ack invariant also forbids final Resume=done
until Next==Queued+1, preventing omission of the remaining managed sink chunks.
A read-only archive of baseline commit9f7b50f is prepared under /tmp for missing
historical extended measurements; the canonical worktree has not been reset.

Round3 correctness found a callback-time receipt corruption shortcut in
persistSinkResult: Complete could be returned while durable job remained ack.
The implementation now correlates job/stage and active ownership before every
receipt read/result merge shortcut. Permanent callback fault-injection regression
and targeted purge/Sweep suite passed. Independent round4 closure is requested.
The running after matrix was compiled before this change; its purge/Sweep samples
are stage evidence and need rerunning on the corrected snapshot. Final acceptance
and task02 commit remain pending.

Final acceptance achieved on the corrected source snapshot: completeness100%
(ST01–10), separate correctness review reports zero confirmed open errors.
Final whole race rerun passed root492.373s/all packages. The prior graph test
cutoff failure is preserved; its10k corpus and all assertions were unchanged.
Final vet/format/examples pass;63-case comparison and24-case corrected replacements
plus20×3 contention are terminal and independently verified. Task02 is ready for
its single commit;03/04 have not started.
