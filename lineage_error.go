package memy

import "errors"

// Missing, revoked or replaced dependencies are ineligible. Unknown storage,
// schema and cancellation failures must still propagate instead of empty recall.
func ineligibleLineage(err error) bool {
	return errors.Is(err, ErrStaleInput) && knownLineageCause(err)
}

func knownLineageCause(err error) bool {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		for _, cause := range joined.Unwrap() {
			if !knownLineageCause(cause) {
				return false
			}
		}
		return true
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return knownLineageCause(wrapped.Unwrap())
	}
	return errors.Is(err, ErrStaleInput) || errors.Is(err, ErrNotFound) || errors.Is(err, ErrRevoked)
}
