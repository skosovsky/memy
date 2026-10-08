//go:build e2e

package checkpoint_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/examples/managed-projections/checkpoint"
	"github.com/skosovsky/memy/examples/managed-projections/host"
	"github.com/skosovsky/memy/reference"
)

// TestE2ECheckpointBackendProcess is also the separate service process entry point.
func TestE2ECheckpointBackendProcess(t *testing.T) {
	if os.Getenv("MEMY_CHECKPOINT_SERVICE") != "1" {
		return
	}
	store, err := checkpoint.Open(context.Background(), os.Getenv("MEMY_CHECKPOINT_DB"), "remote-checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var dropped atomic.Bool
	handler := checkpoint.Handler(store, "test-host-credential")
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/502" {
			handler.ServeHTTP(&lostReply{header: make(http.Header)}, r)
			http.Error(w, "gateway lost committed response", http.StatusBadGateway)
			return
		}
		if r.URL.Path == "/drop" && !dropped.Swap(true) {
			// Commit the actual request in the durable backend, then lose its reply.
			handler.ServeHTTP(&lostReply{header: make(http.Header)}, r)
			hijacker, ok := w.(http.Hijacker)
			if !ok {
				t.Error("no hijacker")
				return
			}
			conn, _, hijackErr := hijacker.Hijack()
			if hijackErr != nil {
				t.Error(hijackErr)
				return
			}
			_ = conn.Close()
			return
		}
		handler.ServeHTTP(w, r)
	})
	server := &http.Server{Handler: wrapped, ReadHeaderTimeout: time.Second}
	fmt.Println("http://" + listener.Addr().String())
	if err = server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		t.Fatal(err)
	}
}

type lostReply struct{ header http.Header }

func (w *lostReply) Header() http.Header       { return w.header }
func (*lostReply) WriteHeader(int)             {}
func (*lostReply) Write(b []byte) (int, error) { return len(b), nil }

type service struct {
	remote  checkpoint.Remote
	cmd     *exec.Cmd
	stopped bool
}

func startService(t *testing.T, path string) *service {
	t.Helper()
	cmd := exec.CommandContext(
		t.Context(),
		os.Args[0],
		"-test.run=^TestE2ECheckpointBackendProcess$",
		"-test.timeout=2m",
	)
	cmd.Env = append(os.Environ(), "MEMY_CHECKPOINT_SERVICE=1", "MEMY_CHECKPOINT_DB="+path)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	address, err := bufio.NewReader(pipe).ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		t.Fatal(err)
	}
	s := &service{
		remote: checkpoint.Remote{
			Endpoint: strings.TrimSpace(address),
			Token:    "test-host-credential",
			Identity: "remote-checkpoint",
			Client:   &http.Client{Timeout: time.Second},
		},
		cmd: cmd,
	}
	t.Cleanup(func() { s.stop() })
	return s
}
func (s *service) stop() {
	if !s.stopped {
		s.stopped = true
		_ = s.cmd.Process.Kill()
		_ = s.cmd.Wait()
	}
}

func TestE2ERemoteDurablePurgeUnknownReplyRestartAndFence(t *testing.T) {
	// Arrange: an actual TCP service in a separate OS process writes real SQLite.
	path := filepath.Join(t.TempDir(), "remote.db")
	server := startService(t, path)
	sc := memy.Scope{Tenant: "a", Namespace: "knowledge", Subject: "user"}
	fence := memy.EpochFence{Scope: sc, Epoch: 0}
	artifact := checkpoint.Artifact{
		Scope:   sc,
		Handle:  "same",
		Lineage: []memy.RevisionRef{{RecordID: "a", Revision: 1}, {RecordID: "b", Revision: 1}},
		Data:    []byte("checkpoint"),
	}
	key, err := server.remote.Put(t.Context(), fence, artifact)
	if err != nil {
		t.Fatal(err)
	}
	foreign := artifact
	foreign.Scope.Tenant = "other"
	foreignKey, err := server.remote.Put(t.Context(), memy.EpochFence{Scope: foreign.Scope}, foreign)
	if err != nil {
		t.Fatal(err)
	}
	batch := memy.PurgeBatch{
		OperationID: "forget-a",
		Scope:       sc,
		Epoch:       1,
		Selector:    memy.Selector{Kind: memy.SelectRecord, ID: "a"},
		Records:     []string{"a"},
		Chunk:       1,
	}
	drop := server.remote
	drop.Endpoint += "/drop"
	// Act: purge commits but the HTTP reply is lost; kill/restart the service.
	_, err = drop.Purge(t.Context(), batch)
	// Assert: caller has no complete receipt, while committed deletion persists.
	if !errors.Is(err, memy.ErrUnknownOutcome) {
		t.Fatalf("lost response err=%v", err)
	}
	server.stop()
	restarted := startService(t, path)
	if _, err = restarted.remote.Load(t.Context(), sc, key); !errors.Is(err, memy.ErrNotFound) {
		t.Fatalf("deleted checkpoint restored: %v", err)
	}
	ack, err := restarted.remote.Purge(t.Context(), batch)
	if err != nil || ack.Sink != "remote-checkpoint" || ack.Epoch != batch.Epoch || ack.Chunk != batch.Chunk ||
		ack.OperationID != batch.OperationID {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
	if _, err = restarted.remote.Put(t.Context(), fence, artifact); !errors.Is(err, memy.ErrStaleInput) {
		t.Fatalf("late retry err=%v", err)
	}
	if _, err = restarted.remote.Load(t.Context(), foreign.Scope, foreignKey); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err = restarted.remote.Purge(cancelled, batch); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel err=%v", err)
	}
	unauthorized := restarted.remote
	unauthorized.Token = "wrong"
	if _, err = unauthorized.Purge(t.Context(), batch); err == nil {
		t.Fatal("unauthenticated service request accepted")
	}
}

func bindRemote(t *testing.T, s *host.Session, remote checkpoint.Remote) {
	t.Helper()
	engine, err := memy.New(
		memy.Config[string, string, string]{
			Store:          s.Canonical,
			Authority:      s.Policy,
			Sources:        s.Sources,
			Clock:          s.Clock,
			Retention:      reference.Retain[string]{Version: "retain"},
			PayloadCodec:   memy.JSONCodec[string]{},
			ReferenceCodec: memy.JSONCodec[string]{},
			Sinks:          []memy.Sink{remote},
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	s.Engine = engine
}

func TestE2ERemoteCanonicalForgetReceiptLossRecovery(t *testing.T) {
	// Arrange: canonical storage and its registered checkpoint sink live in distinct processes.
	dir := t.TempDir()
	backendPath := filepath.Join(t.TempDir(), "remote.db")
	server := startService(t, backendPath)
	session, err := host.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	sc := memy.Scope{Tenant: "tenant", Namespace: "knowledge", Subject: "user"}
	if err = session.Provision(sc); err != nil {
		t.Fatal(err)
	}
	drop := server.remote
	drop.Endpoint += "/drop"
	bindRemote(t, session, drop)
	ref, err := session.Seed(t.Context(), sc, "fact", "canonical knowledge")
	if err != nil {
		t.Fatal(err)
	}
	fence, result, err := session.Prepare(t.Context(), sc, []memy.RevisionRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var key string
	err = session.Engine.WithDerivedWrite(
		t.Context(),
		"reader",
		fence,
		host.Purpose,
		[]memy.RevisionRef{ref},
		func(ctx context.Context) error {
			var putErr error
			key, putErr = server.remote.Put(
				ctx,
				fence,
				checkpoint.Artifact{Scope: sc, Handle: "context", Lineage: []memy.RevisionRef{ref}, Data: raw},
			)
			return putErr
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// Act: canonical revoke commits and remote deletion commits, but its reply is lost.
	pending, err := session.Forget(t.Context(), sc, ref)
	// Assert: unknown sink outcome cannot produce complete, even when deletion really happened.
	if err != nil || pending.State == memy.PurgeComplete || !pending.CanonicalComplete {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	server.stop()
	if err = session.Close(); err != nil {
		t.Fatal(err)
	}
	recoveredBackend := startService(t, backendPath)
	recovered, err := host.Open(t.Context(), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = recovered.Close() })
	if err = recovered.Provision(sc); err != nil {
		t.Fatal(err)
	}
	bindRemote(t, recovered, recoveredBackend.remote)
	complete, err := recovered.Forget(t.Context(), sc, ref)
	if err != nil || complete.State != memy.PurgeComplete {
		t.Fatalf("recovery=%+v err=%v", complete, err)
	}
	if _, err = checkpoint.ReadValidated(
		t.Context(),
		recoveredBackend.remote,
		recovered.Engine,
		"reader",
		sc,
		host.Purpose,
		key,
	); err == nil {
		t.Fatal("remote revoked checkpoint delivered")
	}
	_, after, err := recovered.Prepare(t.Context(), sc, []memy.RevisionRef{ref})
	if err != nil || len(after.Projections) != 0 {
		t.Fatalf("late index=%+v err=%v", after, err)
	}
}

func TestE2ERemoteGatewayStatusAfterDurableCommit(t *testing.T) {
	// Arrange: a real backend commits Put before a gateway replaces its status.
	server := startService(t, filepath.Join(t.TempDir(), "gateway.db"))
	sc := memy.Scope{Tenant: "tenant", Namespace: "knowledge", Subject: "user"}
	artifact := checkpoint.Artifact{
		Scope:   sc,
		Handle:  "committed",
		Lineage: []memy.RevisionRef{{RecordID: "a", Revision: 1}},
		Data:    []byte("secret"),
	}
	gateway := server.remote
	gateway.Endpoint += "/502"
	// Act.
	_, err := gateway.Put(t.Context(), memy.EpochFence{Scope: sc}, artifact)
	// Assert: the operation is unknown, not assumed rolled back or unapplied.
	if !errors.Is(err, memy.ErrUnknownOutcome) || !errors.Is(err, memy.ErrUnavailable) {
		t.Fatalf("gateway err=%v", err)
	}
	key, err := checkpoint.Key(artifact)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := server.remote.Load(t.Context(), sc, key)
	if err != nil || string(loaded.Data) != "secret" {
		t.Fatalf("committed data=%q err=%v", loaded.Data, err)
	}
}
