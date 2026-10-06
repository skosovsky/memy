package memy

import (
	"reflect"
	"testing"
	"time"
)

func projectionIdentityFixture() (*Engine[string, string, string], Record[string, string]) {
	instant := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	engine := &Engine[string, string, string]{
		config: Config[string, string, string]{PayloadCodec: JSONCodec[string]{}, ReferenceCodec: JSONCodec[string]{}},
	}
	record := Record[string, string]{
		ID:         "fact",
		Revision:   1,
		Scope:      Scope{Tenant: "A", Namespace: "facts", Subject: "user"},
		Payload:    "payload",
		State:      Active,
		ObservedAt: instant,
		RecordedAt: instant,
		Valid:      Interval{Known: true, From: instant, To: instant.Add(time.Hour)},
		Retention: Retention{
			PolicyVersion: "retention/v1",
			ExpiresAt:     instant.Add(time.Hour),
		},
		ExpiresAt:              instant.Add(time.Hour),
		AuthorityPolicyVersion: "authority/v1",
		Epoch:                  1,
		Reconciliation: &Reconciliation{
			Mode:          Append,
			Related:       []RevisionRef{{RecordID: "ancestor", Revision: 1}},
			PolicyVersion: "resolver/v1",
			Basis:         "basis",
		},
		Provenance: Provenance[string]{
			Sources:       []Source[string]{{ID: "source", Revision: "r1", Reference: "ref"}},
			Extractor:     "extractor/v1",
			Evidence:      "evidence",
			Lineage:       []RevisionRef{{RecordID: "ancestor", Revision: 1}},
			Losses:        []string{"loss"},
			Uncertainties: []string{"uncertain"},
		},
	}
	return engine, record
}

func TestProjectionSnapshotCoversAllRecordFields(t *testing.T) {
	// Arrange: exhaustively track the public metadata shape, including future fields.
	engine, original := projectionIdentityFixture()
	mutations := map[string]func(*Record[string, string]){
		"ID":                     func(r *Record[string, string]) { r.ID = "changed" },
		"Revision":               func(r *Record[string, string]) { r.Revision++ },
		"Scope":                  func(r *Record[string, string]) { r.Scope.Subject = "changed" },
		"Payload":                func(r *Record[string, string]) { r.Payload = "changed" },
		"State":                  func(r *Record[string, string]) { r.State = Conflicted },
		"Provenance":             func(r *Record[string, string]) { r.Provenance.Evidence = "changed" },
		"ObservedAt":             func(r *Record[string, string]) { r.ObservedAt = r.ObservedAt.Add(time.Second) },
		"RecordedAt":             func(r *Record[string, string]) { r.RecordedAt = r.RecordedAt.Add(time.Second) },
		"Valid":                  func(r *Record[string, string]) { r.Valid.Known = false },
		"Retention":              func(r *Record[string, string]) { r.Retention.PolicyVersion = "changed" },
		"ExpiresAt":              func(r *Record[string, string]) { r.ExpiresAt = r.ExpiresAt.Add(time.Second) },
		"AuthorityPolicyVersion": func(r *Record[string, string]) { r.AuthorityPolicyVersion = "changed" },
		"Epoch":                  func(r *Record[string, string]) { r.Epoch++ },
		"Reconciliation":         func(r *Record[string, string]) { r.Reconciliation.Basis = "changed" },
	}
	shape := reflect.TypeFor[Record[string, string]]()
	if len(mutations) != shape.NumField() {
		t.Fatal("snapshot coverage must include every Record field")
	}
	for member := range shape.Fields() {
		field := member.Name
		mutate := mutations[field]
		if mutate == nil {
			t.Fatalf("missing snapshot fixture: %s", field)
		}
		t.Run(field, func(t *testing.T) { assertProjectionSnapshotMutation(t, engine, original, mutate) })
	}
}

func assertProjectionSnapshotMutation(
	t *testing.T,
	engine *Engine[string, string, string],
	original Record[string, string],
	mutate func(*Record[string, string]),
) {
	t.Helper()
	// Arrange.
	before, err := engine.projectionSnapshot(original)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := engine.cloneRecord(original)
	if err != nil {
		t.Fatal(err)
	}
	// Act.
	mutate(&changed)
	after, err := engine.projectionSnapshot(changed)
	// Assert.
	if err != nil || before == after {
		t.Fatalf("snapshot missed changed input: %v", err)
	}
}

func TestProjectionSnapshotCoversNestedInputs(t *testing.T) {
	// Arrange.
	engine, original := projectionIdentityFixture()
	mutations := map[string]func(*Record[string, string]){
		"source_id":              func(r *Record[string, string]) { r.Provenance.Sources[0].ID = "changed" },
		"source_revision":        func(r *Record[string, string]) { r.Provenance.Sources[0].Revision = "changed" },
		"source_reference":       func(r *Record[string, string]) { r.Provenance.Sources[0].Reference = "changed" },
		"extractor":              func(r *Record[string, string]) { r.Provenance.Extractor = "changed" },
		"lineage":                func(r *Record[string, string]) { r.Provenance.Lineage[0].Revision++ },
		"losses":                 func(r *Record[string, string]) { r.Provenance.Losses[0] = "changed" },
		"uncertainties":          func(r *Record[string, string]) { r.Provenance.Uncertainties[0] = "changed" },
		"valid_from":             func(r *Record[string, string]) { r.Valid.From = r.Valid.From.Add(time.Second) },
		"valid_to":               func(r *Record[string, string]) { r.Valid.To = r.Valid.To.Add(time.Second) },
		"retention_deadline":     func(r *Record[string, string]) { r.Retention.ExpiresAt = r.Retention.ExpiresAt.Add(time.Second) },
		"reconciliation_mode":    func(r *Record[string, string]) { r.Reconciliation.Mode = Conflict },
		"reconciliation_related": func(r *Record[string, string]) { r.Reconciliation.Related[0].Revision++ },
		"reconciliation_policy":  func(r *Record[string, string]) { r.Reconciliation.PolicyVersion = "changed" },
	}
	// Act / Assert.
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) { assertProjectionSnapshotMutation(t, engine, original, mutate) })
	}
}

type opaqueProjectionValue struct {
	Text     string
	Callback func()
}
type opaqueProjectionCodec struct{}

func (opaqueProjectionCodec) Version() string { return "opaque/v1" }
func (opaqueProjectionCodec) Encode(value opaqueProjectionValue) ([]byte, error) {
	return []byte(value.Text), nil
}
func (opaqueProjectionCodec) Decode(raw []byte) (opaqueProjectionValue, error) {
	return opaqueProjectionValue{Text: string(raw), Callback: func() {}}, nil
}

func TestProjectionSnapshotUsesBYOTCodecs(t *testing.T) {
	// Arrange: consumer representation cannot be encoded by encoding/json directly.
	engine := &Engine[opaqueProjectionValue, opaqueProjectionValue, string]{
		config: Config[opaqueProjectionValue, opaqueProjectionValue, string]{
			PayloadCodec:   opaqueProjectionCodec{},
			ReferenceCodec: opaqueProjectionCodec{},
		},
	}
	record := Record[opaqueProjectionValue, opaqueProjectionValue]{
		Payload: opaqueProjectionValue{Text: "payload", Callback: func() {}},
		Provenance: Provenance[opaqueProjectionValue]{
			Sources: []Source[opaqueProjectionValue]{
				{Reference: opaqueProjectionValue{Text: "reference", Callback: func() {}}},
			},
		},
	}
	// Act.
	before, err := engine.projectionSnapshot(record)
	if err != nil {
		t.Fatal(err)
	}
	record.Payload.Text = "changed"
	after, err := engine.projectionSnapshot(record)
	// Assert: the canonical codec bytes, not a presumed JSON shape, define identity.
	if err != nil || before == after {
		t.Fatalf("BYOT snapshot: %v", err)
	}
}
