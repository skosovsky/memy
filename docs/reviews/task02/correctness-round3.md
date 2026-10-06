# Task 02 — independent retry-path correlation review, round 3

Reviewer: task02_correctness; no implementation participation, edits or commits. This targeted interim round follows the added readPurge/Sweep stage correlation and preserves the earlier R3–R5 closure evidence. It is not final task acceptance. Reviewed-file SHA-256 snapshot: correctness-round3-snapshot.sha256.

## Checked code

readPurge now validates receipt binding plus the persisted job's stage before returning a receipt to sink delivery. Sweep adoption loads the corresponding job and rejects a complete receipt inconsistent with that job or still-owned active purge pointer. The completed-job replay cases distinguish older completed operations from unrelated later active operations. Existing progression/authority/phase/Next guards remain present.

## Confirmed remaining issue — sink persistence shortcut skips stage correlation

`persistSinkResult` still returns immediately when the reloaded receipt has a different chunk or state complete/revocation_committed, without loading its job or checking stage correlation. A receipt can change while an external sink callback is in flight, after readPurge validated its earlier state. A state shortcut therefore needs the same persisted cross-document validation before accepting the current receipt as a result.

Independent overlay reproduction TestReviewCompletedReceiptDuringAck:

1. Seed one canonical record and configure a sink that injects a privileged durable metadata fault during its callback.
2. Advance native Limit=1 purge until its acknowledged-emission phase is pending; canonical cleanup is already complete.
3. The sink marks only the persisted receipt complete and its sink acknowledged. The job remains phase ack, and the active purge pointer still owns this operation.
4. On callback return, persistSinkResult takes its complete-state shortcut. Public Forget returns PurgeComplete with no error, but Fence still returns ErrRevoked.

This is a privileged metadata-corruption probe, not an untrusted normal sink capability exploit. It violates the checked durable progress/receipt/fence consistency model and the requested review of all retry paths. No canonical-content deletion is needed to reproduce it.

Command: `GOCACHE=/tmp/memy-go-build GOPATH=/tmp/memy-gopath go test -overlay <temporary overlay> -run '^TestReviewCompletedReceiptDuringAck$' -v .`. Probe overlays a nonexistent root review_round3_test.go; exact source in correctness-round3-probes.go.txt, output in correctness-round3-reproduction.txt. Reproduction passed, meaning the inconsistent false completion was observed.

Required closure: reload/validate exact bound job and receipt stage before every early return in acknowledgement persistence; maintain completed/unfinished ownership consistency on read/result paths; verify corruption after initial checked read gives ErrSchema, with no newly persisted acknowledgement/fence retirement. Legitimate stale acknowledgements after a newer chunk or an already finished operation must remain safe/idempotent.

## Contract audit note

The design's remote callback statement should distinguish expensive Ranker/Projector/merge/sink work outside writer transactions from synchronous Authority/Sources/Retention/Clock/codec validation still performed inside Update. These transaction-local ports need explicit bounded/local/context obligations; an unrestricted remote implementation can otherwise hold SQLite's single writer while checking policy. Do not claim arbitrary callbacks can be forced to stop, nor move checks in a way that loses canonical final revalidation. This is a contract clarification, not a reproduced regression finding in the new readPurge/Sweep changes.

Decision: targeted round not accepted until the persistence shortcut is correlated with the job/fence. Whole-task final race and performance gates remain pending; no duplicate whole race or benchmark was launched.
