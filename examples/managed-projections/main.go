// Command managed-projections demonstrates durable host-owned checkpoint cleanup.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/examples/managed-projections/checkpoint"
	"github.com/skosovsky/memy/examples/managed-projections/host"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "memy-managed-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	session, err := host.Open(ctx, dir)
	if err != nil {
		return err
	}
	defer func() { _ = session.Close() }()
	scope := memy.Scope{Tenant: "tenant", Namespace: "knowledge", Subject: "user"}
	if err = session.Provision(scope); err != nil {
		return err
	}
	ref, err := session.Seed(ctx, scope, "fact", "canonical knowledge, not an instruction")
	if err != nil {
		return err
	}
	fence, result, err := session.Prepare(ctx, scope, []memy.RevisionRef{ref})
	if err != nil {
		return err
	}
	key, err := session.Persist(ctx, fence, "conversation/checkpoint", result)
	if err != nil {
		return err
	}
	if err = session.Close(); err != nil {
		return err
	}
	session, err = host.Open(ctx, dir)
	if err != nil {
		return err
	}
	if err = session.Provision(scope); err != nil {
		return err
	}
	if _, err = checkpoint.ReadValidated(
		ctx,
		session.Checkpoints,
		session.Engine,
		"reader",
		scope,
		host.Purpose,
		key,
	); err != nil {
		return err
	}
	session.Checkpoints.FailPurge(errors.New("sink temporarily unavailable"))
	receipt, err := session.Forget(ctx, scope, ref)
	if err != nil || receipt.State == memy.PurgeComplete {
		return errors.New("failed sink incorrectly completed purge")
	}
	if _, err = checkpoint.ReadValidated(
		ctx,
		session.Checkpoints,
		session.Engine,
		"reader",
		scope,
		host.Purpose,
		key,
	); err == nil {
		return errors.New("revoked checkpoint served")
	}
	session.Checkpoints.FailPurge(nil)
	receipt, err = session.Forget(ctx, scope, ref)
	if err != nil || receipt.State != memy.PurgeComplete {
		return errors.New("retry did not complete")
	}
	_, recalled, err := session.Prepare(ctx, scope, []memy.RevisionRef{ref})
	if err != nil || len(recalled.Projections) != 0 {
		return errors.New("late search resurrected knowledge")
	}
	fmt.Println(
		"Canonical recall; rank-only evidence; durable checkpoint reopened; pending purge blocked reads; retry completed; stale index filtered.",
	)
	return nil
}
