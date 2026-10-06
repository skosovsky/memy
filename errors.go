package memy

import "errors"

// Stable errors can be inspected through [errors.Is]. Error messages must not
// contain payloads, credentials or existence details from unauthorized scopes.
var (
	ErrUnauthorized       = errors.New("memy: unauthorized")
	ErrScopeViolation     = errors.New("memy: scope violation")
	ErrConflict           = errors.New("memy: version or operation conflict")
	ErrUnsupported        = errors.New("memy: unsupported capability")
	ErrSchema             = errors.New("memy: unsupported schema")
	ErrInvalid            = errors.New("memy: invalid input")
	ErrInterval           = errors.New("memy: invalid valid-time interval")
	ErrNotFound           = errors.New("memy: not found")
	ErrUnavailable        = errors.New("memy: unavailable")
	ErrStaleAcceptance    = errors.New("memy: stale acceptance")
	ErrStaleCursor        = errors.New("memy: stale scan cursor")
	ErrStaleInput         = errors.New("memy: stale input")
	ErrPolicyDenied       = errors.New("memy: policy denied")
	ErrMissingEvidence    = errors.New("memy: missing evidence")
	ErrSourceUnavailable  = errors.New("memy: source unavailable")
	ErrUnresolvedConflict = errors.New("memy: unresolved conflict")
	ErrIncomparable       = errors.New("memy: incomparable claims")
	ErrVisibilityPending  = errors.New("memy: visibility pending")
	ErrBudget             = errors.New("memy: budget exhausted")
	ErrMaintenance        = errors.New("memy: maintenance requires continuation")
	ErrRevoked            = errors.New("memy: revoked")
	ErrUnknownOutcome     = errors.New("memy: commit outcome unknown; retry operation identity")
	ErrClosed             = errors.New("memy: closed")
)
