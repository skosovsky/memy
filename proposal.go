package memy

import (
	"context"
	"errors"
)

const maximumExtractionSuggestions = 256

func proposalContentDigest(p proposalDisk) (string, error) {
	p.Digest = ""
	p.State = Proposed // Acceptance/rejection changes lifecycle, not reviewed content.
	return digest(p)
}

// Revise replaces proposed content, invalidating all earlier acceptance tokens.
// A changed proposal needs a separate new host review before canonical commit.
func (e *Engine[P, R, A]) Revise(
	ctx context.Context,
	authority A,
	scope Scope,
	operationID, id string,
	expected Version,
	purpose string,
	suggestion Suggestion[P, R],
) (Proposal[P, R], error) {
	decision, operationErr := e.authorize(ctx, authority, scope, ActionPropose, purpose)
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	if !validIdentifier(id) {
		return Proposal[P, R]{}, ErrInvalid
	}
	encoded, operationErr := e.encodeSuggestion(suggestion)
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	requestDigest, operationErr := operationDigest(scope, decision.Actor, purpose, struct {
		ID       string
		Expected Version
		Content  proposalDisk
	}{id, expected, encoded})
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	var result Proposal[P, R]
	operationErr = e.config.Store.Update(ctx, scope, func(b Bucket) error {
		op, opVersion, exists, transactionErr := operation(b, operationID, "revise", requestDigest)
		if transactionErr != nil {
			return transactionErr
		}
		var old proposalDisk
		version, transactionErr := readDocument(b, objectKey("proposal", id), "proposal", &old)
		if transactionErr != nil {
			return transactionErr
		}
		if exists {
			replayed, replayErr := e.replayRevised(ctx, b, authority, scope, purpose, decision, op, id, expected)
			result = replayed
			return replayErr
		}
		if revisionErr := proposalRevisionMatches(old, scope, id, expected); revisionErr != nil {
			return revisionErr
		}
		encoded.ID, encoded.Revision, encoded.CreatedAt = id, expected+1, old.CreatedAt
		encoded, transactionErr = e.prepareProposal(ctx, b, scope, encoded)
		if transactionErr != nil {
			return transactionErr
		}
		if err := e.reauthorize(ctx, authority, scope, ActionPropose, purpose, decision); err != nil {
			return err
		}
		if saveErr := saveProposalOperation(
			b,
			operationID,
			"revise",
			requestDigest,
			opVersion,
			version,
			encoded,
		); saveErr != nil {
			return saveErr
		}
		result, transactionErr = e.decodeProposal(encoded)
		return transactionErr
	})
	if operationErr != nil {
		return Proposal[P, R]{}, operationErr
	}
	return result, nil
}

// ExtractionJob is a host-owned provider/job identity. The input digest binds
// codec version, scope and provider identity; scheduling stays outside memy.
type ExtractionJob struct {
	OperationID     string
	ProviderVersion string
	Purpose         string
}

// Extract invokes a typed provider outside the store transaction, then persists
// all proposals atomically only if its captured scope epoch remains current.
func Extract[I, P, R, A any](
	ctx context.Context,
	e *Engine[P, R, A],
	authority A,
	scope Scope,
	job ExtractionJob,
	input I,
	codec Codec[I],
	provider Extractor[I, P, R],
) ([]Proposal[P, R], error) {
	if e == nil || nilPort(codec) || nilPort(provider) || !validIdentifier(job.ProviderVersion) || !validIdentifier(job.OperationID) {
		return nil, ErrInvalid
	}
	decision, authErr := e.authorize(ctx, authority, scope, ActionPropose, job.Purpose)
	if authErr != nil {
		return nil, authErr
	}
	requestDigest, digestErr := extractionRequestDigest(scope, decision.Actor, job, input, codec)
	if digestErr != nil {
		return nil, digestErr
	}
	captured, replayErr := e.extractionReplay(ctx, authority, scope, job, requestDigest, decision)
	if replayErr != nil {
		return nil, replayErr
	}
	if captured.Completed {
		return captured.Proposals, nil
	}
	suggestions, providerErr := provider.Extract(ctx, input)
	if providerErr != nil {
		return nil, providerErr
	}
	if cancelErr := ctx.Err(); cancelErr != nil {
		return nil, cancelErr
	}
	encoded, encodeErr := e.encodeExtraction(job, suggestions)
	if encodeErr != nil {
		return nil, encodeErr
	}
	return e.persistExtraction(ctx, authority, scope, job, requestDigest, decision, captured.Epoch, encoded)
}

func extractionRequestDigest[I any](
	scope Scope,
	actor string,
	job ExtractionJob,
	input I,
	codec Codec[I],
) (string, error) {
	if !validIdentifier(codec.Version()) {
		return "", ErrSchema
	}
	encoded, encodeErr := codec.Encode(input)
	if encodeErr != nil {
		return "", encodeErr
	}
	if len(encoded) == 0 {
		return "", ErrInvalid
	}
	return operationDigest(scope, actor, job.Purpose, struct {
		Input    []byte
		Codec    string
		Provider string
		Scope    Scope
	}{
		encoded, codec.Version(), job.ProviderVersion, scope,
	})
}

type extractionSnapshot[P, R any] struct {
	Epoch     epochDisk
	Proposals []Proposal[P, R]
	Completed bool
}

func (e *Engine[P, R, A]) extractionReplay(
	ctx context.Context,
	authority A,
	scope Scope,
	job ExtractionJob,
	requestDigest string,
	decision Decision,
) (extractionSnapshot[P, R], error) {
	var snapshot extractionSnapshot[P, R]
	viewErr := e.config.Store.View(ctx, scope, func(b Bucket) error {
		op, _, exists, ledgerErr := operation(b, job.OperationID, "extract", requestDigest)
		if ledgerErr != nil {
			return ledgerErr
		}
		if !exists {
			epoch, _, epochErr := currentEpoch(b)
			snapshot.Epoch = epoch
			return epochErr
		}
		proposals, loadErr := e.loadProposals(ctx, b, scope, op.Proposals)
		if loadErr != nil {
			return loadErr
		}
		snapshot.Proposals, snapshot.Completed = proposals, true
		return e.reauthorize(ctx, authority, scope, ActionPropose, job.Purpose, decision)
	})
	if viewErr != nil {
		return extractionSnapshot[P, R]{}, viewErr
	}
	return snapshot, nil
}

func (e *Engine[P, R, A]) encodeExtraction(job ExtractionJob, suggestions []Suggestion[P, R]) ([]proposalDisk, error) {
	if len(suggestions) == 0 {
		return nil, ErrMissingEvidence
	}
	if len(suggestions) > maximumExtractionSuggestions {
		return nil, ErrBudget
	}
	encoded := make([]proposalDisk, 0, len(suggestions))
	for _, suggestion := range suggestions {
		suggestion.Extractor = job.ProviderVersion
		p, encodeErr := e.encodeSuggestion(suggestion)
		if encodeErr != nil {
			return nil, encodeErr
		}
		encoded = append(encoded, p)
	}
	return encoded, nil
}

func (e *Engine[P, R, A]) persistExtraction(
	ctx context.Context,
	authority A,
	scope Scope,
	job ExtractionJob,
	requestDigest string,
	decision Decision,
	captured epochDisk,
	encoded []proposalDisk,
) ([]Proposal[P, R], error) {
	var result []Proposal[P, R]
	updateErr := e.config.Store.Update(ctx, scope, func(b Bucket) error {
		op, version, exists, ledgerErr := operation(b, job.OperationID, "extract", requestDigest)
		if ledgerErr != nil {
			return ledgerErr
		}
		refs := op.Proposals
		if !exists {
			prepared, prepareErr := e.prepareExtraction(ctx, b, scope, job, captured, encoded)
			if prepareErr != nil {
				return prepareErr
			}
			refs = prepared
			if saveErr := writeDocument(b, objectKey("operation", job.OperationID), "operation", version, operationDisk{
				Action: "extract", Digest: requestDigest, Proposals: refs, Receipt: nil, Epoch: nil,
			}); saveErr != nil {
				return saveErr
			}
		}
		if authErr := e.reauthorize(ctx, authority, scope, ActionPropose, job.Purpose, decision); authErr != nil {
			return authErr
		}
		proposals, loadErr := e.loadProposals(ctx, b, scope, refs)
		if loadErr != nil {
			return loadErr
		}
		result = proposals
		return nil
	})
	if updateErr != nil {
		return nil, updateErr
	}
	return result, nil
}

func (e *Engine[P, R, A]) prepareExtraction(
	ctx context.Context,
	b Bucket,
	scope Scope,
	job ExtractionJob,
	captured epochDisk,
	encoded []proposalDisk,
) ([]ProposalRef, error) {
	epoch, _, epochErr := currentEpoch(b)
	if epochErr != nil {
		return nil, epochErr
	}
	if epoch.Value != captured.Value {
		return nil, ErrStaleInput
	}
	refs := make([]ProposalRef, 0, len(encoded))
	for index, p := range encoded {
		p.ID, _ = digest(struct {
			Operation string
			Index     int
			Scope     Scope
		}{job.OperationID, index, scope})
		p.Revision = 1
		prepared, prepareErr := e.prepareProposal(ctx, b, scope, p)
		if prepareErr != nil {
			return nil, prepareErr
		}
		if saveErr := persistProposal(b, prepared, 0); saveErr != nil {
			return nil, saveErr
		}
		refs = append(refs, ProposalRef{ID: prepared.ID, Revision: prepared.Revision})
	}
	return refs, nil
}

func (e *Engine[P, R, A]) loadProposals(
	ctx context.Context,
	b Bucket,
	scope Scope,
	refs []ProposalRef,
) ([]Proposal[P, R], error) {
	result := make([]Proposal[P, R], 0, len(refs))
	for _, ref := range refs {
		var p proposalDisk
		if _, err := readDocument(b, proposalRevisionKey(ref), "proposal", &p); err != nil {
			return nil, errors.Join(ErrRevoked, err)
		}
		if p.Scope != scope || p.ID != ref.ID || p.Revision != ref.Revision {
			return nil, ErrSchema
		}
		if retentionErr := e.validateRetention(ctx, scope, p); retentionErr != nil {
			return nil, retentionErr
		}
		decoded, err := e.decodeProposal(p)
		if err != nil {
			return nil, err
		}
		result = append(result, decoded)
	}
	return result, nil
}
