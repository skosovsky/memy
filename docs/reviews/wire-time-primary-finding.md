# Primary-agent finding: C9 wire timestamp normalization

During remediation of independent C7/C8, the primary agent actively reproduced
another schema/runtime mismatch in an external scratch test (not attributed to
either independent reviewer). Original scratch source:
/private/tmp/memy-correctness-round2/time_shape_test.go.

A committed record's history document was changed only at recorded_at to
`2026-06-01T00:00:00,0Z`. Go time decoding normalized the decimal comma to the
original instant and Get returned payload with nil error. The wire schema uses
RFC3339 date-time; the executable schema validator rejects this representation.
The scratch contract assertion failed before the fix. No ACL bypass or change
in decoded payload was inferred.

Spec-first remediation adds requiredTime shape validation before Go decoding,
rejecting decimal comma and invalid timezone hour/minute offsets. The new
TestTimestampWireFormatMatchesExecutableSchema checks both actual executable
schema rejection and runtime ErrSchema for comma/+00:60/+24:00 inputs. This
applies to all persisted time.Time fields, rather than a recorded_at special
case. Null content-free tombstone timestamps retain their specified zero-time
strings.

Status: addressed locally; independent closure pending round 3. The added
regression passed along with the current full go test ./.... Final acceptance
must include independent validation of C9, not merely the primary's statement.
