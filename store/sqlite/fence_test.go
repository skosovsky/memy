package sqlite_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/skosovsky/memy"
	"github.com/skosovsky/memy/store/sqlite"
)

func TestFencedProcessHelper(t *testing.T) {
	path := os.Getenv("MEMY_FENCE_CHILD_PATH")
	if path == "" {
		t.Skip("subprocess helper")
	}
	s, err := sqlite.Open(context.Background(), path, sqlite.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	err = s.FencedView(context.Background(), testScope(), func(memy.Bucket) error {
		fmt.Fprintln(os.Stdout, "fenced")
		_, copyErr := io.Copy(io.Discard, os.Stdin)
		return copyErr
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestScopeFencingAcrossProcesses(t *testing.T) {
	for _, crash := range []bool{false, true} {
		t.Run(fmt.Sprintf("crash-%t", crash), func(t *testing.T) {
			// Arrange: another process holds the exact scope delivery fence.
			path := filepath.Join(t.TempDir(), "process.db")
			s := open(t, path, sqlite.Options{})
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd, stdin, finished := startFencedChild(ctx, t, path)

			// Act: same-scope mutation waits; another scope can commit meanwhile.
			probe, probeCancel := context.WithTimeout(ctx, 40*time.Millisecond)
			entered := false
			operationErr := s.Update(probe, testScope(), func(memy.Bucket) error { entered = true; return nil })
			probeCancel()
			if entered || !errors.Is(operationErr, context.DeadlineExceeded) {
				t.Fatalf("same scope entered=%t err=%v", entered, operationErr)
			}
			other := testScope()
			other.Subject = "independent"
			if err := s.Update(
				ctx,
				other,
				func(b memy.Bucket) error { _, err := b.Put("key", 0, []byte("live")); return err },
			); err != nil {
				t.Fatal(err)
			}
			if crash {
				_ = cmd.Process.Kill()
			} else {
				_ = stdin.Close()
			}
			waitErr := cmd.Wait()
			*finished = true
			if !crash && waitErr != nil {
				t.Fatal(waitErr)
			}

			// Assert: release/process death removes exclusion without a stale lease.
			if err := s.Update(
				ctx,
				testScope(),
				func(b memy.Bucket) error { _, err := b.Put("after", 0, []byte("committed")); return err },
			); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCursorSurvivesHandleReopenButNotMutationOrDatabaseCopy(t *testing.T) {
	// Arrange: a cursor does not retain its originating database handle.
	path := filepath.Join(t.TempDir(), "cursor.db")
	a := open(t, path, sqlite.Options{})
	if err := a.Update(context.Background(), testScope(), func(b memy.Bucket) error {
		for _, key := range []string{"prefix/a", "prefix/b"} {
			if _, err := b.Put(key, 0, []byte("v")); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	options := memy.ScanOptions{Prefix: "prefix/", Limit: 1, MaxBytes: 100}
	if err := a.View(
		context.Background(),
		testScope(),
		func(b memy.Bucket) error { p, err := b.Scan(options); options.Cursor = p.Cursor; return err },
	); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}

	// Act/Assert: reopen binds the same physical database and persisted generation.
	b := open(t, path, sqlite.Options{})
	if err := b.View(context.Background(), testScope(), func(bucket memy.Bucket) error {
		p, err := bucket.Scan(options)
		if err == nil && (len(p.Entries) != 1 || p.Entries[0].Key != "prefix/b" || !p.Complete) {
			t.Fatalf("resume: %+v", p)
		}
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	checkCopiedDatabaseCursor(t, path, options)
	c := open(t, path, sqlite.Options{})
	if err := c.Update(
		context.Background(),
		testScope(),
		func(bucket memy.Bucket) error { _, err := bucket.Delete("prefix/b", 1); return err },
	); err != nil {
		t.Fatal(err)
	}
	if err := c.View(context.Background(), testScope(), func(bucket memy.Bucket) error {
		_, err := bucket.Scan(options)
		if !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("stale cursor: %v", err)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func startFencedChild(ctx context.Context, t *testing.T, path string) (*exec.Cmd, io.WriteCloser, *bool) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestFencedProcessHelper$")
	cmd.Env = append(os.Environ(), "MEMY_FENCE_CHILD_PATH="+path)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if startErr := cmd.Start(); startErr != nil {
		t.Fatal(startErr)
	}
	finished := new(bool)
	t.Cleanup(func() {
		if !*finished {
			_ = stdin.Close()
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
	ready := make(chan bool, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if scanner.Text() == "fenced" {
				ready <- true
				return
			}
		}
		ready <- false
	}()
	select {
	case ok := <-ready:
		if !ok {
			t.Fatal("child did not fence")
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	return cmd, stdin, finished
}

func checkCopiedDatabaseCursor(t *testing.T, path string, options memy.ScanOptions) {
	t.Helper()
	copyPath := filepath.Join(t.TempDir(), "copy.db")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if writeErr := os.WriteFile(copyPath, raw, 0600); writeErr != nil {
		t.Fatal(writeErr)
	}
	copied := open(t, copyPath, sqlite.Options{})
	if viewErr := copied.View(context.Background(), testScope(), func(bucket memy.Bucket) error {
		_, err := bucket.Scan(options)
		if !errors.Is(err, memy.ErrStaleCursor) {
			t.Fatalf("copy cursor: %v", err)
		}
		return nil
	}); viewErr != nil {
		t.Fatal(viewErr)
	}
}
