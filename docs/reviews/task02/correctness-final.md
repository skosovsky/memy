# Task02 — independent final correctness review

Reviewer: task02_final_correctness. No participation in implementation; production and permanent tests were read only. Only this report, source snapshot and targeted check logs were written by the reviewer. This is final correctness acceptance for the reviewed current source snapshot.

## Reviewed scope

Read task02, prior correctness rounds1–4 and their independent probes; current Store/Bucket/cursor contracts; memory transaction overlay and AVL range traversal; SQLite addressed SQL, read/write connection pools, scoped Unix file exclusion and reopen identity; membership proof construction and durable canonical evidence; native purge seed/expand/cleanup/emission/acknowledgement/finalization; Sweep current-policy pass and adoption; Snapshot page eligibility and cursor plan binding; Consolidate addressed exact-input preparation; Recall/Project final fresh canonical revalidation; managed derived writes; runtime document validators and schema3 definitions. Examined permanent batch, callback, corruption, graph, private cost and independently opened SQLite Engine tests.

Source SHA-256: correctness-final-snapshot.json. The parent's final-corrected-source.json was independently compared with every current listed file; zero mismatches. No benchmark or full race suite was duplicated.

## Correctness conclusions from source and targeted checks

Scoped memory exclusion protects shared maps while touched-key overlays retain rollback and detached bytes. SQLite atomic addressed CAS retains tombstone versions and generation invalidation, while independent handles coordinate exact-scope FencedView/Update through kernel flock rather than process-local mutexes. WAL read transactions do not retain the global writer reservation through a held managed callback. Unsupported fencing platforms are rejected before database effects. The documented local-file/path/host trust boundary matters; this is not distributed coordination or a lease.

Canonical input preparation and final result validation stay separate from expensive Ranker/Projector/merge work. Every selected exact revision is loaded again after ranking, or projected exact input checked again, before final delivery. Authority/source/retention/deadline and active maintenance gates fail closed. Data already delivered to the trusted host callback is explicitly outside a subsequent payload recall promise. Managed side effects retain exact-scope exclusion and do not promise exactly-once effects or forced termination of arbitrary Go callbacks.

Historical membership writes are atomic with canonical revisions. Selection first derives the exact canonical relation root, then verifies future edge proofs against content-free evidence bound to operation/scope/revision under the durable maintenance fence. Proof leaf/branch/count domains, odd sibling duplication, position/count shape and depth bound prevent arbitrary edge assertions. The evidence eliminates repeated full fan-in child decoding across small batches/reopen. It does not certify absent edges following arbitrary privileged raw-store deletion; raw Store access remains a documented privileged boundary.

Purge progress applies runtime counter/phase/resume constraints plus receipt/stage/active ownership correlation before advancement and before persistence shortcuts. Cleanup completes before emission. Exact chunk acknowledgements merge only into the matching current batch; final completion retires owned active pointer only after the final emitted chunk is acknowledged. Sweep exact-key validation precedes retention decisions; current host policy is evaluated on each visited revision. Same-actor authority rollout does not alter durable pass identity. Sweep completes a frozen pass, not a continuous assertion that no future policy/expiry changes can occur.

R1 policy rollout, R2 false Sweep done, R3 repeated child decoding, R4 foreign revision-key selection, R5 premature phase/final acknowledgement, and round3 persistence shortcut are closed in the inspected final implementation. No new confirmed unclosed error was identified.

## Independent verification

- Final corrected source hashes match current files.
- Targeted race plus prior adversarial overlay probes: correctness-final-targeted.txt. PASS. The corrupt receipt changed during sink callback is rejected with ErrSchema and the active fence remains. A legitimate late chunk1 acknowledgement leaves pending chunk2 unacknowledged, across memory and independently opened SQLite handles; exact chunk2 acknowledgement completes. Permanent callback/adopted-Sweep guard tests also pass.
- Separate independently opened SQLite Engine idempotency test: correctness-final-cross-handle.txt; PASS. The exact same concurrent Commit and bounded Forget must leave one revision2 Revoked tombstone, no canonical history and identical purge epoch/chunk.

Parent-provided final-checks.json and empty final-format/final-vet logs record format/vet PASS; lifecycle and quality example output records successful executable examples. Final performance report/method/raw data explicitly separate completed from censored observations, retain all 63 cases with three repetitions, disclose handle-cold rather than physical disk-cold conditions and physical SQLite writer serialization. Changed purge persistence paths were remeasured for all24 affected cases; retained39 cases do not call changed functions. Private counters represent returned values/decode/copy work, not database VM or physical I/O.

The final whole race gate passed: go test -race -timeout=20m ./..., terminal exit0, root492.373s, all packages successful (final-race.txt / final-checks.json). Parent terminal session99673 confirmation was obtained; the saved output was independently inspected and contains no FAIL. Current final-corrected-source.json hashes again match every listed file. Fresh vet/format after the test-only change passed; executable example paths remain byte-identical to their successful runs. This report does not certify absence of all possible defects beyond the inspected and tested scope.

## Final race retry note

The first final race run failed solely because the native SQLite10k graph reached its local three-minute context deadline (final-race-timeout-stage.txt). This is not accepted as a passing general gate. The graph test now uses t.Context and the overall go test timeout20m. The10k chain, historical head, branches/cycle, bounded production traversal, exact tombstone revisions, complete erasure, unrelated survival and replay assertions remain unchanged. Removing a workstation-sensitive race timing bound does not reduce the graph completion requirement or substitute a smaller workload; wall-clock thresholds are expressly not CI performance gates in task02.

Only purge_graph_test.go changed among files already in the reviewer snapshot. Production, schemas and benchmark harness hashes are unchanged. Parent final-corrected-source hashes again match every current listed file. The updated correctness-final-snapshot.json identifies the revised test. The rerun general race is now terminal PASS; no additional full race or benchmark was launched by this reviewer.

## Final decision

Ошибок в проверенном объёме не обнаружено. Все ранее подтверждённые замечания закрыты; незакрытых подтверждённых ошибок нет. Задача02 принята по корректности на состоянии correctness-final-snapshot.json, согласованном с final-corrected-source.json и финальными проверками. Полнота требований и процент выполнения принадлежат отдельному независимому ревьюеру; это заключение не подменяет его матрицу.
