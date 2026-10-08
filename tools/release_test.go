package tools_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type releaseFixture struct {
	repo, remote, base, script string
	env                        []string
}

func newReleaseFixture(t *testing.T) releaseFixture {
	t.Helper()
	base := t.TempDir()
	f := releaseFixture{
		repo:   filepath.Join(base, "repo"),
		remote: filepath.Join(base, "remote.git"),
		base:   base,
		script: filepath.Join(rootDir(t), "scripts", "release.sh"),
	}
	if err := os.Mkdir(f.repo, 0o755); err != nil {
		t.Fatal(err)
	}
	// Git process configuration is detached from the developer's identity/signing.
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "GIT_") {
			f.env = append(f.env, item)
		}
	}
	f.env = append(f.env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "TMPDIR="+base)
	f.git(t, "init", "-q", "--bare", f.remote)
	f.git(t, "init", "-q", f.repo)
	f.git(t, "config", "user.name", "Fixture")
	f.git(t, "config", "user.email", "fixture@example.invalid")
	writeFile(t, filepath.Join(f.repo, "go.mod"), "module example.invalid/memy\n\ngo 1.27.1\n", 0o644)
	writeFile(
		t,
		filepath.Join(f.repo, "Makefile"),
		"print-publishable-modules:\n\t@echo .\ncheck:\n\t@test ! -f check-fails\nrelease-candidate:\n\t@test ! -f candidate-fails\ntest-published:\n\t@test ! -f proxy-fails\n",
		0o644,
	)
	f.git(t, "add", "go.mod", "Makefile")
	f.git(t, "commit", "-qm", "fixture")
	f.git(t, "remote", "add", "origin", f.remote)
	return f
}

func (f releaseFixture) git(t *testing.T, args ...string) string {
	t.Helper()
	return mustRun(t, f.repo, f.env, "git", args...)
}
func (f releaseFixture) release(t *testing.T, args ...string) (string, error) {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "bash", append([]string{f.script}, args...)...)
	cmd.Dir = f.repo
	cmd.Env = f.env
	cmd.Stdin = strings.NewReader("y\n")
	out, err := cmd.CombinedOutput()
	return string(out), err
}
func (f releaseFixture) snapshot(t *testing.T) string {
	t.Helper()
	index, err := os.ReadFile(filepath.Join(f.repo, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := os.ReadFile(filepath.Join(f.repo, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	return f.git(
		t,
		"rev-parse",
		"HEAD",
	) + f.git(
		t,
		"status",
		"--porcelain",
		"--untracked-files=all",
	) + f.git(
		t,
		"show-ref",
	) + string(
		index,
	) + string(
		manifest,
	)
}
func (f releaseFixture) tags(t *testing.T) string {
	t.Helper()
	return f.git(t, "ls-remote", "--tags", "--refs", f.remote)
}
func (f releaseFixture) wrapper(t *testing.T, mode string) releaseFixture {
	t.Helper()
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(f.base, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(bin, "git"), `#!/bin/bash
for arg in "$@"; do
 if [[ "$arg" == tag && "$FIXTURE_MODE" == before-tag ]]; then exit 1; fi
 if [[ "$arg" == push ]]; then
  touch "$FIXTURE_MARKER"
  if [[ "$FIXTURE_MODE" == unknown ]]; then exit 1; fi
  "$REAL_GIT" "$@"
  exit 1
 fi
done
if [[ "$1" == ls-remote && "$FIXTURE_MODE" == unknown && -f "$FIXTURE_MARKER" ]]; then exit 1; fi
exec "$REAL_GIT" "$@"
`, 0o755)
	f.env = append(
		f.env,
		"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
		"REAL_GIT="+realGit,
		"FIXTURE_MODE="+mode,
		"FIXTURE_MARKER="+filepath.Join(f.base, "pushed"),
	)
	return f
}

func TestReleaseScenarios(t *testing.T) {
	scenarios := []string{
		"happy",
		"break",
		"rejected-retry",
		"detached",
		"before-tag",
		"lost-response",
		"unknown",
		"dirty",
		"major-path",
		"unsupported",
		"check-failure",
		"candidate-failure",
		"proxy-recovery",
		"source",
		"conflict",
		"legacy",
		"lock",
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			// Arrange
			f := newReleaseFixture(t)
			var handled bool
			f, handled = f.prepareScenario(t, scenario)
			if handled {
				return
			}
			before := f.snapshot(t)
			kind := "patch"
			if scenario == "break" || scenario == "unsupported" {
				kind = "break"
			}
			// Act
			out, err := f.release(t, kind, ".")
			// Assert
			failure := map[string]bool{"rejected-retry": true, "before-tag": true, "unknown": true, "dirty": true, "major-path": true, "unsupported": true, "check-failure": true, "candidate-failure": true, "proxy-recovery": true, "conflict": true, "lock": true}[scenario]
			if failure != (err != nil) {
				t.Fatalf("unexpected result %v\n%s", err, out)
			}
			if f.snapshot(t) != before {
				t.Fatal("release mutated the caller checkout/index/refs")
			}
			f.assertScenario(t, scenario)
			if scenario == "rejected-retry" || scenario == "unknown" || scenario == "proxy-recovery" ||
				scenario == "conflict" {
				f.checkRecovery(t, scenario, out)
			}
		})
	}
}

func (f releaseFixture) checkRecovery(t *testing.T, scenario, out string) {
	t.Helper()
	records, globErr := filepath.Glob(filepath.Join(f.base, "memy-release.*"))
	if globErr != nil || len(records) != 1 {
		t.Fatalf("missing recovery: %v %v\n%s", records, globErr, out)
	}
	state := records[0]
	if scenario == "rejected-retry" {
		if err := os.Remove(filepath.Join(f.remote, "hooks", "pre-receive")); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "proxy-recovery" {
		if _, err := f.release(t, "finish", state); err == nil {
			t.Fatal("proxy failure reported success")
		}
		return
	}
	if scenario == "conflict" {
		f.git(t, "--git-dir="+f.remote, "fetch", f.repo, "HEAD:refs/tags/v0.0.1")
		f.git(
			t,
			"--git-dir="+f.remote,
			"update-ref",
			"refs/tags/v0.0.1",
			f.git(t, "hash-object", "-w", "go.mod"),
		)
		if _, err := f.release(t, "resume", state); err == nil {
			t.Fatal("conflicting identity accepted")
		}
		return
	}
	f.env = append(f.env, "FIXTURE_MODE=normal")
	// The unknown wrapper deliberately makes push return failure even after commit.
	if resumed, resumeErr := f.release(t, "resume", state); resumeErr != nil {
		t.Fatalf("resume: %v\n%s", resumeErr, resumed)
	}
	if inspected, inspectErr := f.release(
		t,
		"inspect",
		state,
	); inspectErr != nil ||
		!strings.Contains(inspected, "complete") {
		t.Fatalf("inspect: %v %s", inspectErr, inspected)
	}
}

func (f releaseFixture) prepareScenario(t *testing.T, scenario string) (releaseFixture, bool) {
	t.Helper()
	switch scenario {
	case "happy":
		writeFile(t, filepath.Join(f.repo, "untracked.txt"), "private", 0o644)
		f.git(t, "tag", "scratch")
		f.git(t, "tag", "v0.9.9")
	case "break":
		f.git(t, "tag", "v0.2.8")
		f.git(t, "push", "-q", "origin", "refs/tags/v0.2.8")
	case "detached":
		f.git(t, "checkout", "-q", "--detach", "HEAD")
	case "before-tag", "lost-response", "unknown":
		f = f.wrapper(t, scenario)
	case "rejected-retry":
		writeFile(t, filepath.Join(f.remote, "hooks", "pre-receive"), "#!/bin/sh\nexit 1\n", 0o755)
	case "dirty":
		writeFile(t, filepath.Join(f.repo, "go.mod"), "module changed.invalid/memy\n", 0o644)
	case "major-path":
		f.git(t, "tag", "v2.0.0")
		f.git(t, "push", "-q", "origin", "refs/tags/v2.0.0")
	case "unsupported":
		f.git(t, "tag", "v1.0.0")
		f.git(t, "push", "-q", "origin", "refs/tags/v1.0.0")
	case "check-failure", "candidate-failure", "proxy-recovery":
		name := map[string]string{"check-failure": "check-fails", "candidate-failure": "candidate-fails", "proxy-recovery": "proxy-fails"}[scenario]
		writeFile(t, filepath.Join(f.repo, name), "failure", 0o644)
		f.git(t, "add", name)
		f.git(t, "commit", "-qm", "failure")
	case "source":
		f.env = append(f.env, "RELEASE_SOURCE="+f.git(t, "rev-parse", "HEAD"))
		writeFile(t, filepath.Join(f.repo, "later"), "new", 0o644)
		f.git(t, "add", "later")
		f.git(t, "commit", "-qm", "later")
	case "conflict":
		f = f.wrapper(t, "unknown")
	case "legacy":
		state := filepath.Join(f.base, "legacy")
		if err := os.Mkdir(state, 0o755); err != nil {
			t.Fatal(err)
		}
		writeFile(t, filepath.Join(state, "record.json"), "{}", 0o644)
		if out, err := f.release(t, "resume", state); err == nil || !strings.Contains(out, "previous tooling") {
			t.Fatalf("legacy: %v %s", err, out)
		}
		return f, true
	case "lock":
		lock := filepath.Join(f.repo, ".git", "memy-release.lock")
		if err := os.Mkdir(lock, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f, false
}

func (f releaseFixture) assertScenario(t *testing.T, scenario string) {
	t.Helper()
	if (scenario == "check-failure" || scenario == "candidate-failure" || scenario == "before-tag" || scenario == "dirty") &&
		f.tags(t) != "" {
		t.Fatal("failed preparation published tags")
	}
	if scenario == "happy" && !strings.Contains(f.tags(t), "refs/tags/v0.0.1") {
		t.Fatal("wrong patch")
	}
	if scenario == "break" && !strings.Contains(f.tags(t), "refs/tags/v0.3.0") {
		t.Fatal("wrong minor")
	}
	if scenario == "source" {
		ref := f.git(t, "--git-dir="+f.remote, "ls-tree", "--name-only", "v0.0.1")
		if strings.Contains(ref, "later") {
			t.Fatal("published HEAD instead of selected source")
		}
	}
	if scenario == "dirty" {
		f.git(t, "add", "go.mod")
		if _, err := f.release(t, "patch", "."); err == nil {
			t.Fatal("staged changes accepted")
		}
	}
}
