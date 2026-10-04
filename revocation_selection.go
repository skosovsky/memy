package memy

import "time"

type revocationSelection struct {
	Heads    map[string]recordDisk
	Versions map[string]Version
	Selected map[string]bool
}

func selectRevocations(b Bucket, scope Scope, selector Selector) (revocationSelection, error) {
	entries, listErr := b.List("head/")
	if listErr != nil {
		return revocationSelection{}, listErr
	}
	selection := revocationSelection{
		Heads:    make(map[string]recordDisk, len(entries)),
		Versions: make(map[string]Version, len(entries)),
		Selected: make(map[string]bool),
	}
	all := make([]recordDisk, 0, len(entries))
	for _, entry := range entries {
		var record recordDisk
		if decodeErr := decodeDocument(entry.Value.Data, "record", &record); decodeErr != nil {
			return revocationSelection{}, decodeErr
		}
		if record.Scope != scope || entry.Key != objectKey("head", record.ID) {
			return revocationSelection{}, ErrSchema
		}
		selection.Heads[record.ID], selection.Versions[record.ID] = record, entry.Value.Version
		if (selector.Kind == SelectRecord && record.ID == selector.ID) || selectedRecord(record, selector, nil) {
			selection.Selected[record.ID] = true
		}
		all = append(all, record)
	}
	history, historyErr := revocationHistory(b, scope, selector, selection.Selected)
	if historyErr != nil {
		return revocationSelection{}, historyErr
	}
	all = append(all, history...)
	selectDependentRecords(all, selector, selection.Selected)
	return selection, nil
}

// Historical source and lineage membership also selects the current record ID.
func revocationHistory(b Bucket, scope Scope, selector Selector, selected map[string]bool) ([]recordDisk, error) {
	entries, listErr := b.List("record/")
	if listErr != nil {
		return nil, listErr
	}
	all := make([]recordDisk, 0, len(entries))
	for _, entry := range entries {
		var record recordDisk
		if decodeErr := decodeDocument(entry.Value.Data, "record", &record); decodeErr != nil {
			return nil, decodeErr
		}
		if record.Scope != scope || entry.Key != revisionKey(record.ID, record.Revision) {
			return nil, ErrSchema
		}
		if selectedRecord(record, selector, nil) {
			selected[record.ID] = true
		}
		all = append(all, record)
	}
	return all, nil
}

func selectDependentRecords(records []recordDisk, selector Selector, selected map[string]bool) {
	for changed := true; changed; {
		changed = false
		for _, record := range records {
			if !selected[record.ID] && selectedRecord(record, selector, selected) {
				selected[record.ID], changed = true, true
			}
		}
	}
}

func revokeRecord(b Bucket, record recordDisk, headVersion Version, now time.Time, policy string) error {
	if record.State == Revoked {
		return nil
	}
	if record.Revision >= MaxVersion {
		return ErrConflict
	}
	entries, listErr := b.List(objectKey("record", record.ID) + "/")
	if listErr != nil {
		return listErr
	}
	for _, entry := range entries {
		if _, deleteErr := b.Delete(entry.Key, entry.Value.Version); deleteErr != nil {
			return deleteErr
		}
	}
	// Drop every content-bearing field; the ledger and receipt hold metadata only.
	var emptyProposal proposalDisk
	tombstone := recordDisk{
		ID: record.ID, Revision: record.Revision + 1, Scope: record.Scope,
		State: Revoked, InitialState: Revoked, RecordedAt: now, PolicyVersion: policy,
		Proposal: emptyProposal, Related: nil, Lineage: nil, Transitions: nil,
	}
	return writeDocument(b, objectKey("head", record.ID), "record", headVersion, tombstone)
}
