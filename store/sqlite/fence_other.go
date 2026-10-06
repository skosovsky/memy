//go:build !darwin && !linux && !freebsd && !openbsd && !netbsd && !dragonfly

package sqlite

import (
	"context"

	"github.com/skosovsky/memy"
)

const platformFencing = false

func databaseIdentity(string) (string, error) { return "", memy.ErrUnsupported }

func (*Store) scopeFence(context.Context, memy.Scope, bool) (func(), error) {
	return nil, memy.ErrUnsupported
}
