package memy

// LifecycleCapabilities separates canonical temporal and field guarantees from
// the low-level store profile. Search visibility is advertised by Search.
type LifecycleCapabilities struct {
	Store           StoreCapabilities
	ValidTime       bool
	RecordedTime    bool
	FieldProjection bool
}

// Capabilities reports executable guarantees of the configured engine. The
// engine evaluates temporal predicates over its own canonical revision history.
func (e *Engine[P, R, A]) Capabilities() LifecycleCapabilities {
	return LifecycleCapabilities{
		Store: e.config.Store.Capabilities(), ValidTime: true, RecordedTime: true, FieldProjection: false,
	}
}
