package quality

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	"github.com/skosovsky/memy"
)

func (f *procedureFixture) recordMatches(r memy.Record[ProcedureObservation, DocumentRef]) bool {
	receipt, ok := f.receipts[r.ID]
	return ok && r.Revision == receipt.Revision && reflect.DeepEqual(r.Payload, f.payloads[r.ID]) &&
		r.Scope == f.scope &&
		r.Valid.Known &&
		r.Valid.From.Equal(f.clock.Now()) &&
		len(r.Provenance.Sources) == 1 &&
		reflect.DeepEqual(r.Provenance.Sources[0], f.source) &&
		r.Provenance.Extractor == f.ports.ExtractorVersion &&
		len(r.Provenance.Lineage) == 0 &&
		slices.Equal(r.Provenance.Uncertainties, []string{procedureUncertainty}) &&
		slices.Equal(r.Provenance.Losses, []string{procedureFormattingLoss}) &&
		r.Provenance.Evidence == procedureEvidence &&
		r.ObservedAt.Equal(f.clock.Now()) &&
		r.RecordedAt.Equal(f.recordedAt[r.ID]) &&
		r.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour))
}

func (f *procedureFixture) projectionMatches(p memy.Projection[ProcedureObservation, DocumentRef]) bool {
	r, ok := f.receipts[p.RecordID]
	return ok && p.Revision == r.Revision && reflect.DeepEqual(p.Output, f.payloads[p.RecordID]) &&
		p.Scope == f.scope &&
		p.Trust == "data" &&
		len(p.Provenance.Sources) == 1 &&
		reflect.DeepEqual(p.Provenance.Sources[0], f.source) &&
		p.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour)) &&
		len(p.Provenance.Lineage) == 0 &&
		p.Provenance.Extractor == f.ports.ExtractorVersion &&
		slices.Equal(p.Provenance.Uncertainties, []string{procedureUncertainty}) &&
		slices.Equal(p.Provenance.Losses, []string{procedureFormattingLoss}) &&
		p.Provenance.Evidence == procedureEvidence
}

// Serialized body bytes include all projection/provenance and omission envelopes.
func procedureBodyBytes(body memy.ProjectedRecallResult[ProcedureObservation, DocumentRef]) (uint64, error) {
	raw, err := json.Marshal(body)
	return uint64(len(raw)), err
}

func (f *procedureFixture) canonical(ctx context.Context, report *ScenarioReport, expected int) error {
	records, err := fullSnapshot(
		ctx,
		f.engine,
		"operator",
		f.scope,
		memy.ReadOptions{
			Purpose:          qualityPurpose,
			IncludeConflicts: true,
			ValidAsOf:        time.Time{},
			RecordedAsOf:     time.Time{},
			IncludeUnknown:   false,
		},
	)
	if err != nil {
		return err
	}
	ok := len(records) == expected
	for _, r := range records {
		ok = ok && f.recordMatches(r)
	}
	report.Canonical.Checks = append(
		report.Canonical.Checks,
		procedureCheck("canonical-state", ok, "exact-originals-source-retention", len(records)),
	)
	provenanceOK := len(records) == expected
	for _, record := range records {
		provenanceOK = provenanceOK && len(record.Provenance.Lineage) == 0 && len(record.Provenance.Sources) == 1 &&
			reflect.DeepEqual(record.Provenance.Sources[0], f.source) &&
			record.Provenance.Extractor == f.ports.ExtractorVersion &&
			record.Valid.Known &&
			record.ExpiresAt.Equal(f.clock.Now().Add(procedureRetentionHours*time.Hour))
	}
	report.Canonical.Checks = append(
		report.Canonical.Checks,
		procedureCheck("canonical-provenance", provenanceOK, "exact-sources-lineage-validity-expiry", len(records)),
	)
	return nil
}

func (f *procedureFixture) checkCurrentRetention(ctx context.Context, r *ScenarioReport) error {
	if err := f.canonical(ctx, r, procedureOriginalCount); err != nil {
		return err
	}
	f.clock.Advance(procedureExpiredHours * time.Hour)
	_, expiryErr := f.engine.Get(
		ctx,
		"operator",
		f.scope,
		procedureRelevantA,
		procedureReadOptions(),
	)
	r.Canonical.Checks = append(
		r.Canonical.Checks,
		procedureCheck("current-retention", errors.Is(expiryErr, memy.ErrNotFound), "expired-current-read-denied", 1),
	)
	return nil
}

func (f *procedureFixture) projectionSetMatches(
	actual []memy.Projection[ProcedureObservation, DocumentRef],
	ids []string,
) bool {
	if len(actual) != len(ids) {
		return false
	}
	expected := map[memy.RevisionRef]bool{}
	for _, id := range ids {
		receipt, ok := f.receipts[id]
		if !ok {
			return false
		}
		expected[memy.RevisionRef{RecordID: id, Revision: receipt.Revision}] = true
	}
	for _, projection := range actual {
		ref := memy.RevisionRef{RecordID: projection.RecordID, Revision: projection.Revision}
		if !expected[ref] || !f.projectionMatches(projection) {
			return false
		}
		delete(expected, ref)
	}
	return len(expected) == 0
}

func (f *procedureFixture) recordSetMatches(
	actual []memy.Ranked[ProcedureObservation, DocumentRef],
	ids []string,
) bool {
	if len(actual) != len(ids) {
		return false
	}
	expected := map[memy.RevisionRef]bool{}
	for _, id := range ids {
		receipt, ok := f.receipts[id]
		if !ok {
			return false
		}
		expected[memy.RevisionRef{RecordID: id, Revision: receipt.Revision}] = true
	}
	for _, item := range actual {
		ref := memy.RevisionRef{RecordID: item.Record.ID, Revision: item.Record.Revision}
		if !expected[ref] || !f.recordMatches(item.Record) {
			return false
		}
		delete(expected, ref)
	}
	return len(expected) == 0
}

func (f *procedureFixture) packedMatches(
	packed, all memy.ProjectedRecallResult[ProcedureObservation, DocumentRef],
	bytes, limit uint64,
) bool {
	ok := len(packed.Projections) == 1 && len(packed.Omissions) == 1 && bytes == packed.Budget.Used && bytes <= limit &&
		packed.Budget.Exact
	if len(packed.Projections) == 1 && len(packed.Omissions) == 1 {
		ok = ok && packed.Projections[0].RecordID == all.Projections[0].RecordID &&
			packed.Projections[0].Revision == all.Projections[0].Revision &&
			packed.Omissions[0].Ref == (memy.RevisionRef{RecordID: all.Projections[1].RecordID, Revision: all.Projections[1].Revision}) &&
			packed.Omissions[0].Reason == memy.OmittedBudget
	}
	for _, p := range packed.Projections {
		ok = ok && f.projectionMatches(p)
	}
	return ok
}

func procedureReadOptions() memy.ReadOptions {
	return memy.ReadOptions{
		Purpose:          qualityPurpose,
		ValidAsOf:        time.Time{},
		RecordedAsOf:     time.Time{},
		IncludeUnknown:   false,
		IncludeConflicts: false,
	}
}

func (f *procedureFixture) currentRecord(
	ctx context.Context,
	id string,
) (memy.Record[ProcedureObservation, DocumentRef], error) {
	return f.engine.Get(ctx, "operator", f.scope, id, procedureReadOptions())
}
