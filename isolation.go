package memy

import "slices"

// cloneRecord gives injected policies their own consumer payload/reference
// objects. Cloning a slice alone would still alias maps and pointers in P/R.
func (e *Engine[P, R, A]) cloneRecord(record Record[P, R]) (Record[P, R], error) {
	payload, operationErr := e.config.PayloadCodec.Encode(record.Payload)
	if operationErr != nil {
		return Record[P, R]{}, operationErr
	}
	record.Payload, operationErr = e.config.PayloadCodec.Decode(payload)
	if operationErr != nil {
		return Record[P, R]{}, operationErr
	}
	record.Related = slices.Clone(record.Related)
	record.Provenance.Lineage = slices.Clone(record.Provenance.Lineage)
	record.Provenance.Losses = slices.Clone(record.Provenance.Losses)
	record.Provenance.Uncertainties = slices.Clone(record.Provenance.Uncertainties)
	record.Provenance.Sources = slices.Clone(record.Provenance.Sources)
	for i := range record.Provenance.Sources {
		encoded, err := e.config.ReferenceCodec.Encode(record.Provenance.Sources[i].Reference)
		if err != nil {
			return Record[P, R]{}, err
		}
		reference, err := e.config.ReferenceCodec.Decode(encoded)
		if err != nil {
			return Record[P, R]{}, err
		}
		record.Provenance.Sources[i].Reference = reference
	}
	return record, nil
}
