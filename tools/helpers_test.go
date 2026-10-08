package tools_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func rootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func command(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	return commandContext(t.Context(), t, dir, env, name, args...)
}

func commandContext(
	ctx context.Context,
	t *testing.T,
	dir string,
	env []string,
	name string,
	args ...string,
) (string, error) {
	t.Helper()
	if name == "go" && os.Getenv("GO") != "" {
		name = os.Getenv("GO")
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func mustRun(t *testing.T, dir string, env []string, name string, args ...string) string {
	t.Helper()
	out, err := command(t, dir, env, name, args...)
	if err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
	return strings.TrimSpace(out)
}

func writeFile(t *testing.T, name, body string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(name, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
}

func mustGoContext(ctx context.Context, t *testing.T, dir string, env []string, args ...string) string {
	t.Helper()
	out, err := commandContext(ctx, t, dir, env, "go", args...)
	if err != nil {
		t.Fatalf("go %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(out)
}
