package checkpoint

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/skosovsky/memy"
)

const remoteLimit = 8 << 20
const opPut = "put"
const opLoad = "load"
const opPurge = "purge"
const opInvalidate = "invalidate"

// Remote is a trusted host service port; its credential is not tenant authority.
// Serve only over a trusted channel (TLS outside the loopback example).
type Remote struct {
	Endpoint string
	Token    string
	Identity string
	Client   *http.Client
}

type request struct {
	Operation    string            `json:"operation"`
	Epoch        memy.Version      `json:"epoch"`
	Artifact     Artifact          `json:"artifact"`
	Scope        memy.Scope        `json:"scope"`
	Key          string            `json:"key"`
	OperationID  string            `json:"operation_id"`
	SelectorKind memy.SelectorKind `json:"selector_kind"`
	SelectorID   string            `json:"selector_id"`
	Records      []string          `json:"records"`
	Chunk        uint64            `json:"chunk"`
}
type response struct {
	Key      string        `json:"key"`
	Artifact Artifact      `json:"artifact"`
	Ack      memy.PurgeAck `json:"ack"`
	Error    string        `json:"error"`
}

// Name identifies the registered remote checkpoint sink.
func (r Remote) Name() string { return r.Identity }

// Put completes a remote durable transaction or returns an explicitly unknown outcome.
func (r Remote) Put(ctx context.Context, fence memy.EpochFence, a Artifact) (string, error) {
	var input request
	input.Operation, input.Epoch, input.Scope, input.Artifact = opPut, fence.Epoch, fence.Scope, a
	result, err := r.call(ctx, input)
	return result.Key, err
}

// Load is privileged checkpoint access, never an independent eligibility check.
func (r Remote) Load(ctx context.Context, scope memy.Scope, key string) (Artifact, error) {
	var input request
	input.Operation, input.Scope, input.Key = opLoad, scope, key
	result, err := r.call(ctx, input)
	if err == nil {
		expected, validation := Key(result.Artifact)
		if validation != nil || expected != key || result.Artifact.Scope != scope {
			return Artifact{}, memy.ErrSchema
		}
	}
	return result.Artifact, err
}

// Invalidate deletes the exact scoped checkpoint on the service.
func (r Remote) Invalidate(ctx context.Context, scope memy.Scope, key string) error {
	var input request
	input.Operation, input.Scope, input.Key = opInvalidate, scope, key
	_, err := r.call(ctx, input)
	return err
}

// Purge returns only the service's post-commit acknowledgement.
func (r Remote) Purge(ctx context.Context, batch memy.PurgeBatch) (memy.PurgeAck, error) {
	var input request
	input.Operation, input.Scope, input.Epoch, input.OperationID = opPurge, batch.Scope, batch.Epoch, batch.OperationID
	input.SelectorKind, input.SelectorID, input.Records, input.Chunk = batch.Selector.Kind, batch.Selector.ID, batch.Records, batch.Chunk
	result, err := r.call(ctx, input)
	if err == nil &&
		(result.Ack.Sink != r.Identity || result.Ack.OperationID != batch.OperationID || result.Ack.Epoch != batch.Epoch || result.Ack.Chunk != batch.Chunk) {
		return memy.PurgeAck{}, memy.ErrInvalid
	}
	return result.Ack, err
}

func (r Remote) call(ctx context.Context, input request) (response, error) {
	if r.Client == nil || r.Token == "" || r.Endpoint == "" || r.Identity == "" {
		return response{}, memy.ErrInvalid
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return response{}, err
	}
	if len(raw) > remoteLimit {
		return response{}, memy.ErrBudget
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(raw))
	if err != nil {
		return response{}, err
	}
	req.Header.Set("Authorization", "Bearer "+r.Token)
	req.Header.Set("Content-Type", "application/json")
	reply, err := r.Client.Do(req)
	if err != nil {
		return response{}, errors.Join(memy.ErrUnknownOutcome, err)
	}
	defer func() { _ = reply.Body.Close() }()
	if reply.StatusCode != http.StatusOK {
		return response{}, errors.Join(memy.ErrUnavailable, memy.ErrUnknownOutcome)
	}
	responseBody, err := io.ReadAll(io.LimitReader(reply.Body, remoteLimit+1))
	if err != nil || len(responseBody) > remoteLimit {
		return response{}, errors.Join(memy.ErrUnknownOutcome, err, memy.ErrBudget)
	}
	var result response
	decoder := json.NewDecoder(bytes.NewReader(responseBody))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&result); err != nil {
		return response{}, errors.Join(memy.ErrUnknownOutcome, err)
	}
	var trailing any
	if err = decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return response{}, memy.ErrUnknownOutcome
	}
	return result, remoteError(result.Error)
}

// Handler exposes only a credential-protected privileged host service API.
// The host still supplies authenticated scope and wraps writes/reads in engine gates.
func Handler(s *Store, token string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if token == "" ||
			subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+token)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, remoteLimit)
		defer func() { _ = r.Body.Close() }()
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		var input request
		if err := decoder.Decode(&input); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		var trailing any
		if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
			http.Error(w, "trailing request", http.StatusBadRequest)
			return
		}
		result, err := dispatch(r.Context(), s, input)
		result.Error = remoteCode(err)
		w.Header().Set("Content-Type", "application/json")
		if err = json.NewEncoder(w).Encode(result); err != nil {
			return
		}
	})
}

func dispatch(ctx context.Context, s *Store, input request) (response, error) {
	var result response
	var err error
	switch input.Operation {
	case opPut:
		result.Key, err = s.Put(ctx, memy.EpochFence{Scope: input.Scope, Epoch: input.Epoch}, input.Artifact)
	case opLoad:
		result.Artifact, err = s.Load(ctx, input.Scope, input.Key)
	case opPurge:
		result.Ack, err = s.Purge(
			ctx,
			memy.PurgeBatch{
				OperationID: input.OperationID,
				Scope:       input.Scope,
				Epoch:       input.Epoch,
				Selector:    memy.Selector{Kind: input.SelectorKind, ID: input.SelectorID},
				Records:     input.Records,
				Chunk:       input.Chunk,
			},
		)
	case opInvalidate:
		err = s.Invalidate(ctx, input.Scope, input.Key)
	default:
		err = memy.ErrInvalid
	}
	return result, err
}
func remoteCode(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, memy.ErrStaleInput):
		return "stale"
	case errors.Is(err, memy.ErrConflict):
		return "conflict"
	case errors.Is(err, memy.ErrNotFound):
		return "not_found"
	case errors.Is(err, memy.ErrInvalid):
		return "invalid"
	default:
		return "unknown"
	}
}
func remoteError(code string) error {
	switch code {
	case "":
		return nil
	case "stale":
		return memy.ErrStaleInput
	case "conflict":
		return memy.ErrConflict
	case "not_found":
		return memy.ErrNotFound
	case "invalid":
		return memy.ErrInvalid
	default:
		return memy.ErrUnknownOutcome
	}
}
