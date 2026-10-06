package memy

import (
	"slices"
	"testing"
	"time"
)

func TestConsolidationStampExpiryPreservesExplicitDeadline(t *testing.T) {
	for _, mode := range []ConsolidationMode{ExactDedup, DomainMerge, SemanticMerge} {
		t.Run(string(mode), func(t *testing.T) {
			// Arrange: output already has an earlier explicit deadline than its ancestors.
			start := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
			refs := []RevisionRef{
				{RecordID: "a", Revision: 1},
				{RecordID: "b", Revision: 1},
				{RecordID: "c", Revision: 1},
			}
			merger := extractionMerge[string, string, string]{
				request: ConsolidationRequest{Mode: mode, Inputs: refs, PolicyVersion: "merge/v1"},
				inputs: []Record[string, string]{
					{ID: "a", Revision: 1, ExpiresAt: start.Add(24 * time.Hour)},
					{ID: "b", Revision: 1, ExpiresAt: start.Add(30 * time.Hour)},
					{ID: "c", Revision: 1, ExpiresAt: start.Add(time.Hour)},
				},
			}
			proposal := Suggestion[string, string]{
				Lineage:   slices.Clone(refs[:2]),
				ExpiresAt: start.Add(30 * time.Minute),
			}
			// Act.
			merger.stampSuggestion(&proposal)
			// Assert.
			if !proposal.ExpiresAt.Equal(start.Add(30 * time.Minute)) {
				t.Fatalf("extended explicit deadline: %+v", proposal)
			}
			if mode != ExactDedup && !slices.Equal(proposal.Lineage, refs) {
				t.Fatal("weakened all-input lineage")
			}
			// Arrange / Act: without the explicit bound, domain/semantic still use all inputs.
			proposal.ExpiresAt = time.Time{}
			merger.stampSuggestion(&proposal)
			expected := start.Add(time.Hour)
			if mode == ExactDedup {
				expected = start.Add(24 * time.Hour)
			}
			// Assert.
			if !proposal.ExpiresAt.Equal(expected) {
				t.Fatalf("wrong ancestor bound: %s want %s", proposal.ExpiresAt, expected)
			}
		})
	}
}
