# Round 5 local remediation

C12 independent closure is recorded in correctness-reviewed-lineage.md.
C13 P2 (independent-origin): WithDerivedWrite could invoke its managed callback
after a required input expired during final host reauthorization.

Contract was clarified first. WithDerivedWrite now performs the callback-free
full lineage deadline gate after reauthorization and before fn.
TestManagedWriteRejectsExpiryDuringFinalReauthorization covers direct/transitive
evidence, memory/SQLite, healthy entry and no callback at expiry. Existing
write/revoke race tests remain passing. This does not promise rollback of
external effects after callback entry.

Status: addressed locally, independent closure pending. Full pinned Go 1.26.5
make validate and the external module passed (checks-managed-writes.log, external-managed-writes.log).
