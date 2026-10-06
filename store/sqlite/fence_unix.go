//go:build darwin || linux || freebsd || openbsd || netbsd || dragonfly

package sqlite

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/skosovsky/memy"
)

const platformFencing = true

func databaseIdentity(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", storageError(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", memy.ErrUnsupported
	}
	return fmt.Sprintf("%s/%d/%d", path, stat.Dev, stat.Ino), nil
}

// flock is owned by an open file description, so independently opened handles
// in the same process exclude one another as well as separate processes. Kernel
// cleanup releases locks after process exit; no lease expiration or worker can
// permit a still-running external callback to resurrect a purged artifact.
func (s *Store) scopeFence(ctx context.Context, scope memy.Scope, exclusive bool) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.closed.Load() {
		return nil, memy.ErrClosed
	}
	if err := scope.Validate(); err != nil {
		return nil, err
	}
	hash := sha256.Sum256([]byte(scope.Key()))
	file, err := os.OpenFile(filepath.Join(s.fenceDir, fmt.Sprintf("%x.lock", hash)), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, storageError(err)
	}
	operation := syscall.LOCK_SH | syscall.LOCK_NB
	if exclusive {
		operation = syscall.LOCK_EX | syscall.LOCK_NB
	}
	for {
		if s.closed.Load() {
			_ = file.Close()
			return nil, memy.ErrClosed
		}
		err = syscall.Flock(int(file.Fd()), operation)
		if err == nil {
			if err := ctx.Err(); err != nil {
				_ = file.Close()
				return nil, err
			}
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			_ = file.Close()
			return nil, storageError(err)
		}
		timer := time.NewTimer(lockRetryDelay)
		select {
		case <-ctx.Done():
			timer.Stop()
			_ = file.Close()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}
