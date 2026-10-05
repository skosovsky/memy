package memy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"
)

type document struct {
	Schema uint32          `json:"schema"`
	Kind   string          `json:"kind"`
	Data   json.RawMessage `json:"data"`
}

type sourceDisk struct {
	ID        string `json:"id"`
	Revision  string `json:"revision"`
	Reference []byte `json:"reference"`
}

type proposalDisk struct {
	ID             string        `json:"id"`
	Revision       Version       `json:"revision"`
	Digest         string        `json:"digest"`
	Scope          Scope         `json:"scope"`
	State          ProposalState `json:"state"`
	Payload        []byte        `json:"payload"`
	PayloadCodec   string        `json:"payload_codec"`
	ReferenceCodec string        `json:"reference_codec"`
	Sources        []sourceDisk  `json:"sources"`
	Evidence       string        `json:"evidence"`
	Extractor      string        `json:"extractor"`
	ObservedAt     time.Time     `json:"observed_at"`
	Valid          Interval      `json:"valid"`
	ExpiresAt      time.Time     `json:"expires_at"`
	Lineage        []RevisionRef `json:"lineage"`
	Losses         []string      `json:"losses"`
	Uncertainties  []string      `json:"uncertainties"`
	CreatedAt      time.Time     `json:"created_at"`
	Epoch          Version       `json:"epoch"`
	Retention      Retention     `json:"retention"`
}

type recordDisk struct {
	ID                     string          `json:"id"`
	Revision               Version         `json:"revision"`
	Scope                  Scope           `json:"scope"`
	Proposal               proposalDisk    `json:"proposal"`
	State                  RecordState     `json:"state"`
	InitialState           RecordState     `json:"initial_state"`
	RecordedAt             time.Time       `json:"recorded_at"`
	AuthorityPolicyVersion string          `json:"authority_policy_version"`
	Reconciliation         *Reconciliation `json:"reconciliation"`
	Lineage                []RevisionRef   `json:"lineage"`
	Transitions            []transition    `json:"transitions"`
}

type transition struct {
	At       time.Time   `json:"at"`
	State    RecordState `json:"state"`
	Decision RevisionRef `json:"decision"`
}

type operationDisk struct {
	Action    string         `json:"action"`
	Digest    string         `json:"digest"`
	Proposals []ProposalRef  `json:"proposals"`
	Receipt   *CommitReceipt `json:"receipt"`
	Epoch     *Version       `json:"epoch"`
}

type epochDisk struct {
	Value      Version   `json:"value"`
	RecordedAt time.Time `json:"recorded_at"`
}

func encodeDocument(kind string, value any) ([]byte, error) {
	if !metadataStringsValid(value) {
		return nil, ErrInvalid
	}
	raw, operationErr := json.Marshal(value)
	if operationErr != nil {
		return nil, errors.Join(ErrInvalid, operationErr)
	}
	return json.Marshal(document{Schema: SchemaVersion, Kind: kind, Data: raw})
}

func decodeDocument(raw []byte, kind string, value any) error {
	if !strictWireText(raw) {
		return ErrSchema
	}
	canonical, operationErr := canonicalJSON(raw)
	if operationErr != nil {
		return errors.Join(ErrSchema, operationErr)
	}
	if err := requiredJSONFields(canonical, reflect.TypeFor[document]()); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(canonical))
	decoder.DisallowUnknownFields()
	var doc document
	if err := decoder.Decode(&doc); err != nil {
		return errors.Join(ErrSchema, err)
	}
	if doc.Schema != SchemaVersion || doc.Kind != kind || len(doc.Data) == 0 || doc.Data[0] != '{' {
		return ErrSchema
	}
	decoder = json.NewDecoder(bytes.NewReader(doc.Data))
	if err := requiredJSONFields(doc.Data, reflect.TypeOf(value)); err != nil {
		return err
	}
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return errors.Join(ErrSchema, err)
	}
	if !metadataStringsValid(value) || !validDocument(value) {
		return ErrSchema
	}
	return nil
}

func readDocument(b Bucket, key, kind string, value any) (Version, error) {
	stored, operationErr := b.Get(key)
	if operationErr != nil {
		return 0, operationErr
	}
	if stored.Data == nil {
		return stored.Version, ErrNotFound
	}
	if err := decodeDocument(stored.Data, kind, value); err != nil {
		return 0, err
	}
	return stored.Version, nil
}

func writeDocument(b Bucket, key, kind string, expected Version, value any) error {
	raw, operationErr := encodeDocument(kind, value)
	if operationErr != nil {
		return operationErr
	}
	_, operationErr = b.Put(key, expected, raw)
	return operationErr
}

func objectKey(kind, id string) string {
	hash, _ := digest(id) // Public identity validation precedes key construction.
	return kind + "/" + hash
}

func revisionKey(id string, version Version) string {
	return fmt.Sprintf("%s/%020d", objectKey("record", id), version)
}

func proposalRevisionKey(ref ProposalRef) string {
	return fmt.Sprintf("%s/%020d", objectKey("proposal_revision", ref.ID), ref.Revision)
}

func persistProposal(b Bucket, p proposalDisk, expected Version) error {
	if err := writeDocument(b, objectKey("proposal", p.ID), "proposal", expected, p); err != nil {
		return err
	}
	operationErr := writeDocument(b, proposalRevisionKey(ProposalRef{p.ID, p.Revision}), "proposal", 0, p)
	return operationErr
}

func currentEpoch(b Bucket) (epochDisk, Version, error) {
	var epoch epochDisk
	version, operationErr := readDocument(b, "epoch", "epoch", &epoch)
	if errors.Is(operationErr, ErrNotFound) {
		return epoch, version, nil
	}
	return epoch, version, operationErr
}

func operation(b Bucket, id, action, requestDigest string) (operationDisk, Version, bool, error) {
	if !validIdentifier(id) {
		return operationDisk{}, 0, false, ErrInvalid
	}
	var existing operationDisk
	version, operationErr := readDocument(b, objectKey("operation", id), "operation", &existing)
	if errors.Is(operationErr, ErrNotFound) {
		return existing, version, false, nil
	}
	if operationErr != nil {
		return existing, version, false, operationErr
	}
	if existing.Action != action || existing.Digest != requestDigest {
		return existing, version, false, ErrConflict
	}
	return existing, version, true, nil
}

func operationDigest(scope Scope, actor, purpose string, input any) (string, error) {
	if scope.Validate() != nil || !validIdentifier(actor) || !validPurpose(purpose) || !metadataStringsValid(input) {
		return "", ErrInvalid
	}
	return digest(struct {
		Scope   Scope
		Actor   string
		Purpose string
		Input   any
	}{scope, actor, purpose, input})
}

func recordedTime(now, previous time.Time) time.Time {
	now = now.UTC()
	if !previous.IsZero() && !now.After(previous) {
		return previous.Add(time.Nanosecond)
	}
	return now
}
