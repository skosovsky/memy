//go:build integration

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

func TestIntegrationFencedProcessHelper(t *testing.T) {
	path := os.Getenv("MEMY_FENCE_CHILD_PATH")
	if path == "" {
		return
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

func TestIntegrationScopeFencingAcrossProcesses(t *testing.T) {
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

func startFencedChild(ctx context.Context, t *testing.T, path string) (*exec.Cmd, io.WriteCloser, *bool) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestIntegrationFencedProcessHelper$")
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
