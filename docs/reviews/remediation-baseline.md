# Pre-remediation behavioral evidence

Captured execution against `e12c8fc0d6fc8346e7b67e5a6ecce4c569b932a2`,
Go 1.27.1, darwin/arm64. These are historical runtime failures or diagnostics,
not failures to compile new APIs against old code and not current test results.
Original probe sources and commands remain in the external
[memy review archive](</Users/skosovsky/Library/Mobile Documents/com~apple~CloudDocs/Загрузки/Projects/ai/ai-libs/reviews/memy-2026-10-06/README.md>).

## release

```text
{"case": "success", "exit": 0, "work": "/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/memy-release-review-wyhfo0a5/success", "branch": "main", "local_tags": ["scratch-local", "v0.0.1"], "remote_tags": ["scratch-local", "v0.0.1"], "published_files": ["go.mod", "private-untracked.txt", "release.sh"]}
{"case": "failure", "exit": 1, "work": "/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/memy-release-review-wyhfo0a5/failure", "branch": "", "local_tags": ["v0.0.1"], "remote_tags": []}
evidence_dir=/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/memy-release-review-wyhfo0a5
```

## codec

```text
codec: {"id":9007199254740993} => {"id":9007199254740992} (float64)
unicode 225c756438303022 => "�" error=<nil>
unicode 22ff22 => "�" error=<nil>
Remember→Accept→Commit→Get: {"id":9007199254740993} => {"id":9007199254740992}
```

## lifecycle

```text
=== RUN   TestReviewExactDedupUnrelatedExpiry
    review_lifecycle_probe_test.go:15: lineage=[{a 1} {b 1}] expiry=2026-06-01 01:00:00 +0000 UTC
    review_lifecycle_probe_test.go:16: unrelated record shortened expiry: 2026-06-01 01:00:00 +0000 UTC
--- FAIL: TestReviewExactDedupUnrelatedExpiry (0.01s)
FAIL
FAIL	github.com/skosovsky/memy	0.229s
FAIL
```

## cursor

```text
memory scope validate <nil>
first 1 false cursor bytes 36166 <nil>
resume memy: invalid input
sqlite scope validate <nil>
first 1 false cursor bytes 36166 <nil>
resume memy: invalid input
```

## recall

```text
=== RUN   TestReviewProjectionStateChange
    review_recall_test.go:30: during: Output=active State=conflicted; before=active after=conflicted identicalCacheKey=true
--- PASS: TestReviewProjectionStateChange (0.07s)
PASS
ok  	github.com/skosovsky/memy	1.594s
```

## sweep

```text
=== RUN   TestReviewedSemanticSummaryCommitsAndRetainsOriginals
--- PASS: TestReviewedSemanticSummaryCommitsAndRetainsOriginals (1.47s)
=== RUN   TestReviewSweepDropsCommittedProgressOnError
    review_forget_progress_test.go:36: first=4158eb04b2eafa7a57530cbc412c8f32b642f0b3621083d694f8ea14866e56f1 second=74c9924bda96091d9e63b085e26fa5381bb1bb25447fa5898c4bd92232ca8885 calls=2 result={Records:[] ExpiredProposals:0 Work:0 Complete:false} err=memy: policy denied
        retention unavailable remainingHeads=1 viewErr=<nil>
    review_forget_progress_test.go:40: resumed={Records:[] ExpiredProposals:1 Work:4 Complete:true} err=<nil> observedTotalExpired=1
--- PASS: TestReviewSweepDropsCommittedProgressOnError (0.04s)
PASS
ok  	github.com/skosovsky/memy	2.992s
```

## quality

```text
=== RUN   TestReviewContextBudget
    review_probe_test.go:7: id=procedure-cross-scope context_limit=1 final=pass err=<nil>
    review_probe_test.go:7: id=procedure-poisoning-guarded context_limit=1 final=pass err=<nil>
    review_probe_test.go:7: id=procedure-poisoning-permissive context_limit=1 final=pass err=<nil>
    review_probe_test.go:7: id=procedure-abstention context_limit=1 final=pass err=<nil>
--- PASS: TestReviewContextBudget (0.11s)
=== RUN   TestReviewMissingGrader
    review_probe_test.go:13: grader=claimed-real-grader/v1 callback_nil=true final=pass err=<nil>
--- PASS: TestReviewMissingGrader (0.06s)
=== RUN   TestReviewActualCrossScopeBody
    review_probe_test.go:24: actual cross-scope rendered bytes=1246 projections=1 budget_receipt={Unit: Used:0 Limit:0 Exact:false}
--- PASS: TestReviewActualCrossScopeBody (0.01s)
PASS
ok  	github.com/skosovsky/memy/internal/quality	0.402s
```

## Storage admission and schema probes

Later captured compiled runtime probes against the same baseline; see
remediation decisions D24/D29.

```text
--- FAIL: TestRejectedPutDoesNotClonePayload (0.10s)
    --- FAIL: TestRejectedPutDoesNotClonePayload/key (0.02s)
        bucket_admission_test.go:46: allocs=1.000000 pending=map[]
    --- FAIL: TestRejectedPutDoesNotClonePayload/cancelled (0.03s)
        bucket_admission_test.go:46: allocs=1.000000 pending=map[]
    --- FAIL: TestRejectedPutDoesNotClonePayload/readonly (0.02s)
        bucket_admission_test.go:46: allocs=1.000000 pending=map[]
    --- FAIL: TestRejectedPutDoesNotClonePayload/cas (0.01s)
        bucket_admission_test.go:46: allocs=1.000000 pending=map[]
    --- FAIL: TestRejectedPutDoesNotClonePayload/overflow (0.02s)
        bucket_admission_test.go:46: allocs=1.000000 pending=map[]
FAIL
FAIL	github.com/skosovsky/memy/internal/kv	0.660s
--- FAIL: TestRejectMissingExistingSchemaState (0.36s)
    --- FAIL: TestRejectMissingExistingSchemaState/values (0.06s)
        sqlite_test.go:256: result=&{0x3f091510a270 0x3f091510af70 /private/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/TestRejectMissingExistingSchemaStatevalues2069527949/001/missing.db.memy-fences <nil> {{} {{} 0} {{} {0 0}}} <nil> [31 18 128 157 201 222 131 235 14 14 121 62 129 197 47 232 216 251 185 160 45 1 243 117 34 217 26 178 244 88 123 2] {{} 0}} err=<nil>
    --- FAIL: TestRejectMissingExistingSchemaState/generations (0.03s)
        sqlite_test.go:256: result=&{0x3f0915142000 0x3f0915142d00 /private/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/TestRejectMissingExistingSchemaStategenerations2449790799/001/missing.db.memy-fences <nil> {{} {{} 0} {{} {0 0}}} <nil> [232 209 127 169 98 209 23 204 42 192 46 249 242 232 80 36 186 143 209 83 49 80 120 77 211 116 104 130 130 9 140 91] {{} 0}} err=<nil>
    --- FAIL: TestRejectMissingExistingSchemaState/schema (0.02s)
        sqlite_test.go:256: result=&{0x3f091516c000 0x3f091516cd00 /private/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/TestRejectMissingExistingSchemaStateschema3043168782/001/missing.db.memy-fences <nil> {{} {{} 0} {{} {0 0}}} <nil> [222 142 45 183 175 221 97 254 52 28 117 247 223 223 221 58 202 159 99 171 142 134 24 178 18 248 39 88 18 131 232 31] {{} 0}} err=<nil>
    --- FAIL: TestRejectMissingExistingSchemaState/schema-row (0.04s)
        sqlite_test.go:256: result=&{0x3f0915038c30 0x3f0915039930 /private/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/TestRejectMissingExistingSchemaStateschema-row4129660950/001/missing.db.memy-fences <nil> {{} {{} 0} {{} {0 0}}} <nil> [97 165 193 199 224 162 57 51 202 63 63 161 215 184 37 40 1 116 225 157 69 230 78 197 112 133 99 54 127 78 11 162] {{} 0}} err=<nil>
    --- FAIL: TestRejectMissingExistingSchemaState/columns (0.01s)
        sqlite_test.go:256: result=&{0x3f091516dd40 0x3f0915178b60 /private/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/TestRejectMissingExistingSchemaStatecolumns3526692244/001/missing.db.memy-fences <nil> {{} {{} 0} {{} {0 0}}} <nil> [131 245 236 168 179 78 132 35 249 89 253 173 29 13 66 95 64 242 43 242 2 77 12 111 160 75 126 46 197 201 24 166] {{} 0}} err=<nil>
    --- FAIL: TestRejectMissingExistingSchemaState/primary-key (0.16s)
        sqlite_test.go:256: result=&{0x3f0915179d40 0x3f0915188b60 /private/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/TestRejectMissingExistingSchemaStateprimary-key3301022715/001/missing.db.memy-fences <nil> {{} {{} 0} {{} {0 0}}} <nil> [9 220 219 222 149 17 225 119 123 106 120 213 154 81 24 122 14 7 90 122 162 45 193 31 200 97 233 63 20 23 59 135] {{} 0}} err=<nil>
    --- FAIL: TestRejectMissingExistingSchemaState/types (0.03s)
        sqlite_test.go:256: result=&{0x3f0915189d40 0x3f09150c2a90 /private/var/folders/46/5ywmz5gj26n7mnky51gd60g00000gn/T/TestRejectMissingExistingSchemaStatetypes3520380666/001/missing.db.memy-fences <nil> {{} {{} 0} {{} {0 0}}} <nil> [75 32 42 109 69 192 99 22 173 34 23 196 216 56 228 164 31 172 239 157 33 137 109 20 136 177 46 142 198 102 195 221] {{} 0}} err=<nil>
FAIL
FAIL	github.com/skosovsky/memy/store/sqlite	0.905s
FAIL
```
