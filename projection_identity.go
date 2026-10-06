package memy

// projectionSnapshot identifies every effective field visible to the projector.
// Consumer values use their configured codecs; their Go/JSON shape is irrelevant.
func (e *Engine[P, R, A]) projectionSnapshot(record Record[P, R]) (string, error) {
	payload, err := e.config.PayloadCodec.Encode(record.Payload)
	if err != nil {
		return "", err
	}
	var sources []Source[[]byte]
	if record.Provenance.Sources != nil {
		sources = make([]Source[[]byte], len(record.Provenance.Sources))
	}
	for i, source := range record.Provenance.Sources {
		reference, encodeErr := e.config.ReferenceCodec.Encode(source.Reference)
		if encodeErr != nil {
			return "", encodeErr
		}
		sources[i] = Source[[]byte]{ID: source.ID, Revision: source.Revision, Reference: reference}
	}
	snapshot := Record[[]byte, []byte]{
		ID: record.ID, Revision: record.Revision, Scope: record.Scope, Payload: payload,
		State: record.State, ObservedAt: record.ObservedAt, RecordedAt: record.RecordedAt,
		Valid: record.Valid, Retention: record.Retention, ExpiresAt: record.ExpiresAt,
		AuthorityPolicyVersion: record.AuthorityPolicyVersion, Epoch: record.Epoch,
		Reconciliation: record.Reconciliation,
		Provenance: Provenance[[]byte]{Sources: sources, Extractor: record.Provenance.Extractor,
			Evidence: record.Provenance.Evidence, Lineage: record.Provenance.Lineage,
			Losses: record.Provenance.Losses, Uncertainties: record.Provenance.Uncertainties},
	}
	return digest(struct {
		PayloadCodec   string
		ReferenceCodec string
		Record         Record[[]byte, []byte]
	}{e.config.PayloadCodec.Version(), e.config.ReferenceCodec.Version(), snapshot})
}
