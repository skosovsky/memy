# Task 02 — acknowledgement persistence closure, round 4

Reviewer: task02_correctness; no participation in implementation, no production edits or commits. This round closes the round3 retry-path finding. It is an interim targeted correctness review, not whole-task final acceptance. Reviewed-file hashes are in correctness-round4-snapshot.sha256.

## Changes independently checked

`validateStoredPurge` checks exact job/receipt binding, stage/cleanup/counter correlation, then active ownership. Unfinished receipts must still own their exact active pointer. Completed receipts require a valid done job and cannot retain their own active pointer; a later unrelated operation remains allowed. Both readPurge and persistSinkResult use this helper. Persistence performs it before any stale-chunk/state shortcut or acknowledgement merge, so the current durable state is checked even when a callback returns after another retry changed it.

The finalizer separately reloads the bound ack job, applies runtime invariants and checks active ownership before retirement. Legitimate late results do not override current chunk acknowledgements. The logical complete boundary still requires actual canonical cleanup and every registered sink acknowledgement for every emitted chunk. No exactly-once external-effect promise was added.

## Independent adversarial checks

1. Original round3 callback fault injection, with closure expectations: the sink changes only the persisted receipt to complete while its job remains ack and its active pointer is retained. Public Forget now returns ErrSchema rather than Complete. The active fence remains. Permanent TestPurgeRejectsCompletedReceiptDuringSinkCallback also passes independently.
2. Synchronized legitimate late acknowledgement, memory and SQLite independently opened Engine handles: caller A is held during chunk1 sink processing; caller B confirms chunk1 and advances to pending chunk2; A then returns its valid but old chunk1 acknowledgement. The result stays chunk2 pending and unacknowledged. Only the matching chunk2 acknowledgement completes the purge. No cross-handle global writer callback reservation is required.
3. Permanent TestSweepRejectsCompletedReceiptForUnfinishedPurge passes independently; an inconsistent receipt cannot be adopted as completed cleanup by Sweep.

Command (targeted only):

`GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test -race -overlay <temporary overlay> -run '^(TestReviewCompletedReceiptDuringAckClosed|TestReviewLateAckDoesNotAcceptNextChunk|TestPurgeRejectsCompletedReceiptDuringSinkCallback|TestSweepRejectsCompletedReceiptForUnfinishedPurge)$' -v .`

PASS; output correctness-round4-checks.txt. Exact independent probes: correctness-round4-probes.go.txt, temporarily overlaid onto nonexistent review_round4_test.go. No production/test source files were changed to run them. Test synchronization uses explicit channel boundaries and timeout only to diagnose a stuck test; acceptance assertions depend on durable chunk state, not elapsed latency.

## Contract note closure

The design now explicitly requires transaction-local Authority/Sources/Retention/Clock/codecs to be bounded local operations, to obey context where supplied and to avoid remote I/O/reentrant Store access. The host prepares network policy/source state before entering the engine. Expensive Ranker/Projector/merge remain outside transactions and are followed by fresh canonical checks. This narrows the guarantee to a documented trusted-port contract while preserving BYOT and host responsibility; arbitrary remote callbacks are not claimed to be forcibly cancellable.

Decision: round3 confirmed error closed in this reviewed scope. Ошибок в проверенном объёме не обнаружено. Earlier R3/R4/R5 closure remains valid for the inspected changes. Final acceptance of task02 still requires terminal final-snapshot performance evidence and general checks plus both reviewers' final conclusions. No whole race suite or benchmark was duplicated in parallel with the active measurements.
