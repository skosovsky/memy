//go:build !integration && !e2e

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

type releaseFixture struct{ repo, remote, source, initialRefs string }

func newReleaseFixture(t *testing.T) releaseFixture {
	t.Helper()
	base := t.TempDir()
	repo := filepath.Join(base, "caller")
	remote := filepath.Join(base, "remote.git")
	mustRun(t, base, nil, "git", "init", "--bare", "--quiet", remote)
	mustRun(t, base, nil, "git", "init", "--quiet", "-b", "main", repo)
	for _, pair := range [][2]string{{"user.name", "Fixture"}, {"user.email", "fixture@example.invalid"}, {"commit.gpgsign", "false"}, {"tag.gpgsign", "false"}} {
		mustRun(t, repo, nil, "git", "config", pair[0], pair[1])
	}
	mustRun(t, repo, nil, "git", "remote", "add", "origin", remote)
	write(t, filepath.Join(repo, "scripts/release.sh"), read(t, filepath.Join(rootDir(t), "scripts/release.sh")))
	write(t, filepath.Join(repo, "go.mod"), []byte("module example.invalid/fixture\n\ngo 1.27.1\n"))
	write(t, filepath.Join(repo, "fixture.go"), []byte("package fixture\n"))
	write(
		t,
		filepath.Join(repo, "packages/test/go.mod"),
		[]byte(
			"module example.invalid/fixture/packages/test\n\ngo 1.27.1\n\nrequire example.invalid/fixture v0.0.0\n\nreplace example.invalid/fixture => ../..\n",
		),
	)
	write(
		t,
		filepath.Join(repo, "packages/test/adapter.go"),
		[]byte("package adapter\nimport _ \"example.invalid/fixture\"\n"),
	)
	write(t, filepath.Join(repo, "Makefile"), []byte(`lint test test-integration test-e2e:
	@test ! -f reject-check
modules:
	@printf '.\npackages/test\n'
`))
	mustRun(t, repo, nil, "git", "add", ".")
	mustRun(t, repo, nil, "git", "commit", "--quiet", "-m", "fixture")
	mustRun(t, repo, nil, "git", "push", "origin", "main")
	initialRefs := mustRun(t, repo, nil, "git", "ls-remote", "--refs", remote)
	write(t, filepath.Join(repo, "source.txt"), []byte("unpublished source\n"))
	mustRun(t, repo, nil, "git", "add", "source.txt")
	mustRun(t, repo, nil, "git", "commit", "--quiet", "-m", "source")
	source := mustRun(t, repo, nil, "git", "rev-parse", "HEAD")
	return releaseFixture{repo: repo, remote: remote, source: source, initialRefs: initialRefs}
}

func (f releaseFixture) invoke(t *testing.T, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", append([]string{"scripts/release.sh"}, args...)...)
	isolateProcess(cmd)
	cmd.Dir = f.repo
	cmd.Env = append(os.Environ(), "GOWORK=off", "GONOSUMDB=example.invalid/*")
	cmd.Stdin = strings.NewReader("y\n")
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestReleasePreservesCallerAndPublishesExactRefs(t *testing.T) {
	// Arrange: unrelated local tag and private untracked contents.
	f := newReleaseFixture(t)
	mustRun(t, f.repo, nil, "git", "tag", "unrelated")
	write(t, filepath.Join(f.repo, "private.txt"), []byte("private"))
	before := f.snapshot(t)
	// Act.
	out, err := f.invoke(t, "patch", f.source)
	// Assert.
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD"); got != f.source {
		t.Fatal("caller HEAD changed")
	}
	if got := f.snapshot(t); got != before {
		t.Fatal("caller contents changed")
	}
	refs := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
	if !strings.Contains(refs, "refs/tags/v0.0.1") || strings.Contains(refs, "unrelated") {
		t.Fatalf("wrong refs %s", refs)
	}
	for _, op := range []string{"inspect", "resume", "finish"} {
		if out, err := f.invoke(t, op); err != nil {
			t.Fatalf("%s: %v\n%s", op, err, out)
		}
	}
}

func TestReleaseRejectsDirtySourceAndFailedGate(t *testing.T) {
	for _, mode := range []string{"dirty", "staged", "gate"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			f := newReleaseFixture(t)
			if mode == "gate" {
				write(t, filepath.Join(f.repo, "reject-check"), []byte("fail"))
				mustRun(t, f.repo, nil, "git", "add", "reject-check")
				mustRun(t, f.repo, nil, "git", "commit", "--quiet", "-m", "reject gate")
				f.source = mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD")
			} else {
				write(t, filepath.Join(f.repo, "fixture.go"), []byte("package changed\n"))
				if mode == "staged" {
					mustRun(t, f.repo, nil, "git", "add", "fixture.go")
				}
			}
			// Act.
			out, err := f.invoke(t, "patch", f.source)
			// Assert.
			if err == nil {
				t.Fatalf("unexpected success: %s", out)
			}
			if refs := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote); refs != f.initialRefs {
				t.Fatalf("gate published %s", refs)
			}
		})
	}
}

func TestReleaseRecoveryAfterRejectedPush(t *testing.T) {
	// Arrange: actual remote rejecting hook.
	f := newReleaseFixture(t)
	hook := filepath.Join(f.remote, "hooks/pre-receive")
	write(t, hook, []byte("#!/bin/sh\nexit 1\n"))
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	// Act: failure keeps immutable candidate; subsequent retry publishes that same object.
	out, err := f.invoke(t, "patch", f.source)
	if err == nil {
		t.Fatalf("rejected push succeeded: %s", out)
	}
	record := filepath.Join(f.repo, ".git/library-releases/active")
	candidate := string(read(t, filepath.Join(record, "candidate")))
	if err = os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	if out, err = f.invoke(t, "resume"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	// Assert.
	if string(read(t, filepath.Join(record, "candidate"))) != candidate {
		t.Fatal("retry replaced candidate")
	}
	if !strings.Contains(
		mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote),
		strings.TrimSpace(candidate),
	) {
		t.Fatal("wrong published object")
	}
}

func TestReleaseRejectsChangedCandidateAndUnknownOutcome(t *testing.T) {
	for _, mode := range []string{"candidate", "unknown", "destination", "collision"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange: retain a candidate after rejecting publication.
			f := newReleaseFixture(t)
			hook := filepath.Join(f.remote, "hooks/pre-receive")
			write(t, hook, []byte("#!/bin/sh\nexit 1\n"))
			if err := os.Chmod(hook, 0700); err != nil {
				t.Fatal(err)
			}
			if out, err := f.invoke(t, "patch", f.source); err == nil {
				t.Fatalf("expected rejection: %s", out)
			}
			record := filepath.Join(f.repo, ".git/library-releases/active")
			switch mode {
			case "candidate":
				write(t, filepath.Join(record, "checkout/fixture.go"), []byte("package changed\n"))
			case "unknown":
				write(t, filepath.Join(record, "status"), []byte("unknown\n"))
			case "destination":
				mustRun(t, f.repo, nil, "git", "remote", "set-url", "origin", f.remote+"-other")
			case "collision":
				write(t, filepath.Join(record, "refs"), []byte("refs/tags/v0.0.1 "+strings.Repeat("0", 40)+"\n"))
			}
			// Act / Assert.
			if out, err := f.invoke(t, "resume"); err == nil {
				t.Fatalf("unsafe resume: %s", out)
			}
		})
	}
}

func TestReleaseChecksSelectedSource(t *testing.T) {
	// Arrange: current HEAD differs from the explicitly selected, failing source.
	f := newReleaseFixture(t)
	write(t, filepath.Join(f.repo, "reject-check"), []byte("failure"))
	mustRun(t, f.repo, nil, "git", "add", "reject-check")
	mustRun(t, f.repo, nil, "git", "commit", "--quiet", "-m", "failing source")
	selected := mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD")
	mustRun(t, f.repo, nil, "git", "rm", "reject-check")
	mustRun(t, f.repo, nil, "git", "commit", "--quiet", "-m", "passing head")
	// Act.
	out, err := f.invoke(t, "patch", selected)
	// Assert: checking only the current passing HEAD would incorrectly publish.
	if err == nil {
		t.Fatalf("unverified source published: %s", out)
	}
	if refs := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote); refs != f.initialRefs {
		t.Fatal(refs)
	}
}

func TestReleaseRequiresAtomicPush(t *testing.T) {
	// Arrange: server does not advertise the required atomic capability.
	f := newReleaseFixture(t)
	mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "config", "receive.advertiseAtomic", "false")
	// Act.
	out, err := f.invoke(t, "patch", f.source)
	// Assert: no silent fallback to non-atomic publication.
	if err == nil {
		t.Fatalf("non-atomic release succeeded: %s", out)
	}
	if refs := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote); refs != f.initialRefs {
		t.Fatal(refs)
	}
}

func TestReleaseUnknownRequiresSuccessfulInspection(t *testing.T) {
	// Arrange: prepared candidate after server rejection.
	f := newReleaseFixture(t)
	hook := filepath.Join(f.remote, "hooks/pre-receive")
	write(t, hook, []byte("#!/bin/sh\nexit 1\n"))
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := f.invoke(t, "patch", f.source); err == nil {
		t.Fatal(out)
	}
	moved := f.remote + "-offline"
	if err := os.Rename(f.remote, moved); err != nil {
		t.Fatal(err)
	}
	// Act: unavailable remote observation records unknown.
	if out, err := f.invoke(t, "inspect"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	record := filepath.Join(f.repo, ".git/library-releases/active")
	if strings.TrimSpace(string(read(t, filepath.Join(record, "status")))) != "unknown" {
		t.Fatal("unknown not retained")
	}
	if err := os.Rename(moved, f.remote); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	// Assert: resume remains blocked until inspect resolves uncertainty.
	if out, err := f.invoke(t, "resume"); err == nil {
		t.Fatalf("unknown resume succeeded: %s", out)
	}
	if out, err := f.invoke(t, "inspect"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if out, err := f.invoke(t, "resume"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
}

func TestReleaseRetainsInterruptedCandidate(t *testing.T) {
	// Arrange: emulate interruption after candidate/ref creation, before phase bookkeeping.
	f := newReleaseFixture(t)
	hook := filepath.Join(f.remote, "hooks/pre-receive")
	write(t, hook, []byte("#!/bin/sh\nexit 1\n"))
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := f.invoke(t, "patch", f.source); err == nil {
		t.Fatal(out)
	}
	record := filepath.Join(f.repo, ".git/library-releases/active")
	candidate := string(read(t, filepath.Join(record, "candidate")))
	write(t, filepath.Join(record, "phase"), []byte("preparing\n"))
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	// Act.
	out, err := f.invoke(t, "resume")
	// Assert.
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if string(read(t, filepath.Join(record, "candidate"))) != candidate {
		t.Fatal("candidate replaced after interruption")
	}
}

func TestReleaseRejectsExistingTags(t *testing.T) {
	for _, location := range []string{"local", "remote"} {
		t.Run(location, func(t *testing.T) {
			// Arrange: an annotated tag already reserves the next adapter version.
			f := newReleaseFixture(t)
			tag := "packages/test/v0.0.1"
			mustRun(t, f.repo, nil, "git", "tag", "-a", tag, "-m", "reserved")
			if location == "remote" {
				mustRun(t, f.repo, nil, "git", "push", "origin", "refs/tags/"+tag)
				mustRun(t, f.repo, nil, "git", "tag", "-d", tag)
			}
			before := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
			// Act.
			out, err := f.invoke(t, "patch", f.source)
			// Assert: no overwritten tag and no partial root publication.
			if err == nil || !strings.Contains(out, "tag collision") {
				t.Fatalf("collision accepted: %v %s", err, out)
			}
			if got := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote); got != before {
				t.Fatal("remote changed after collision")
			}
		})
	}
}

func TestReleaseObservesSuccessfulPushDespiteTransportError(t *testing.T) {
	// Arrange: transport reports failure after the real atomic push has completed.
	f := newReleaseFixture(t)
	realGit, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	shim := filepath.Join(bin, "git")
	write(t, shim, []byte(`#!/bin/bash
case " $* " in
*' push --atomic '*) "$REAL_GIT" "$@"; exit 75 ;;
*) exec "$REAL_GIT" "$@" ;;
esac
`))
	if err = os.Chmod(shim, 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("REAL_GIT", realGit)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Act.
	out, err := f.invoke(t, "patch", f.source)
	// Assert: exact remote identities resolve ambiguity, without a second candidate.
	if err != nil || !strings.Contains(out, "Published and verified") {
		t.Fatalf("publication was not observed: %v %s", err, out)
	}
}

func TestReleaseBreakVersionPolicy(t *testing.T) {
	for _, baseline := range []string{"v0.8.1", "v1.2.3"} {
		t.Run(baseline, func(t *testing.T) {
			// Arrange: version selection must use the destination's root tags.
			f := newReleaseFixture(t)
			mustRun(t, f.repo, nil, "git", "tag", baseline)
			mustRun(t, f.repo, nil, "git", "push", "origin", "refs/tags/"+baseline)
			before := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
			// Act.
			out, err := f.invoke(t, "break", f.source)
			// Assert.
			refs := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
			if baseline == "v1.2.3" {
				if err == nil || !strings.Contains(out, "import-version migration") || refs != before {
					t.Fatalf("unprepared major migration: %v %s", err, out)
				}
			} else if err != nil || !strings.Contains(refs, "refs/tags/packages/test/v0.9.0") {
				t.Fatalf("incorrect v0 break: %v %s", err, out)
			}
		})
	}
}

func TestReleasePublishesSourceAndPreparedManifests(t *testing.T) {
	// Arrange: release an older ancestor while the caller remains ahead.
	f := newReleaseFixture(t)
	write(t, filepath.Join(f.repo, "later.txt"), []byte("later\n"))
	mustRun(t, f.repo, nil, "git", "add", "later.txt")
	mustRun(t, f.repo, nil, "git", "commit", "--quiet", "-m", "later")
	head := mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD")
	// Act.
	out, err := f.invoke(t, "patch", f.source)
	// Assert.
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if got := mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "rev-parse", "main"); got != f.source {
		t.Fatalf("main = %s, want source %s", got, f.source)
	}
	if got := mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD"); got != head {
		t.Fatal("local main moved")
	}
	changed := mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "diff", "--name-only", "main", "v0.0.1")
	if strings.Contains(changed, "go.sum") {
		t.Fatalf("release must not manufacture library checksums: %s", changed)
	}
	sourceMod := mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "show", "main:packages/test/go.mod")
	candidateMod := mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "show", "v0.0.1:packages/test/go.mod")
	if !strings.Contains(sourceMod, "replace") || strings.Contains(candidateMod, "replace") ||
		!strings.Contains(candidateMod, "v0.0.1") {
		t.Fatalf("source/candidate manifests: %s / %s", sourceMod, candidateMod)
	}
}

func TestReleaseRejectsInvalidMain(t *testing.T) {
	for _, mode := range []string{"detached", "feature", "foreign-source", "diverged", "missing-remote-main", "legacy-record", "previous-format"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			f := newReleaseFixture(t)
			switch mode {
			case "detached":
				mustRun(t, f.repo, nil, "git", "checkout", "--detach")
			case "feature":
				mustRun(t, f.repo, nil, "git", "checkout", "-b", "feature")
			case "foreign-source":
				mustRun(t, f.repo, nil, "git", "checkout", "-b", "feature")
				mustRun(t, f.repo, nil, "git", "commit", "--allow-empty", "-m", "foreign")
				f.source = mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD")
				mustRun(t, f.repo, nil, "git", "checkout", "main")
			case "diverged":
				mustRun(t, f.repo, nil, "git", "checkout", "-b", "remote-change", "HEAD^")
				mustRun(t, f.repo, nil, "git", "commit", "--allow-empty", "-m", "remote")
				mustRun(t, f.repo, nil, "git", "push", "origin", "HEAD:main")
				mustRun(t, f.repo, nil, "git", "checkout", "main")
			case "missing-remote-main":
				mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "update-ref", "-d", "refs/heads/main")
			case "previous-format":
				write(t, filepath.Join(f.repo, ".git/library-releases/active/format"), []byte("2\n"))
			case "legacy-record":
				if err := os.MkdirAll(filepath.Join(f.repo, ".git/ragy-releases/shell-active"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			before := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
			// Act.
			out, err := f.invoke(t, "patch", f.source)
			// Assert.
			if err == nil {
				t.Fatalf("unsafe release: %s", out)
			}
			if got := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote); got != before {
				t.Fatal("remote changed")
			}
		})
	}
}

func TestReleaseRecoveryAfterMainAdvances(t *testing.T) {
	// Arrange: complete a publication, then move remote main forward.
	f := newReleaseFixture(t)
	if out, err := f.invoke(t, "patch", f.source); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	record := filepath.Join(f.repo, ".git/library-releases/active")
	candidate := string(read(t, filepath.Join(record, "candidate")))
	mustRun(t, f.repo, nil, "git", "commit", "--allow-empty", "-m", "next work")
	mustRun(t, f.repo, nil, "git", "push", "origin", "main")
	before := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
	// Act.
	out, err := f.invoke(t, "resume")
	// Assert: ancestry proves source delivery; recovery must never rewind main.
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if got := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote); got != before {
		t.Fatal("recovery changed refs")
	}
	if got := string(read(t, filepath.Join(record, "candidate"))); got != candidate {
		t.Fatal("candidate changed")
	}
}

func TestReleaseRejectsMainAdvancingDuringPreparation(t *testing.T) {
	// Arrange: a prepared candidate was retained after rejected push.
	f := newReleaseFixture(t)
	hook := filepath.Join(f.remote, "hooks/pre-receive")
	write(t, hook, []byte("#!/bin/sh\nexit 1\n"))
	if err := os.Chmod(hook, 0700); err != nil {
		t.Fatal(err)
	}
	if out, err := f.invoke(t, "patch", f.source); err == nil {
		t.Fatal(out)
	}
	if err := os.Remove(hook); err != nil {
		t.Fatal(err)
	}
	mustRun(t, f.repo, nil, "git", "commit", "--allow-empty", "-m", "newer main")
	mustRun(t, f.repo, nil, "git", "push", "origin", "main")
	before := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
	// Act.
	out, err := f.invoke(t, "resume")
	// Assert: unpublished tags cannot cause the branch to rewind to the older source.
	if err == nil || !strings.Contains(out, "fast-forward") {
		t.Fatalf("%v %s", err, out)
	}
	if got := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote); got != before {
		t.Fatal("remote changed")
	}
}

func TestReleaseRecoversMissingRemoteMainBeforePreparation(t *testing.T) {
	// Arrange: initial remote prerequisite fails before any candidate is prepared.
	f := newReleaseFixture(t)
	baseline := strings.Fields(f.initialRefs)[0]
	mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "update-ref", "-d", "refs/heads/main")
	if out, err := f.invoke(t, "patch", f.source); err == nil {
		t.Fatal(out)
	}
	mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "update-ref", "refs/heads/main", baseline)
	// Act.
	out, err := f.invoke(t, "resume")
	// Assert: initialized inventory permits safe preparation after the prerequisite is restored.
	if err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if got := mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "rev-parse", "main"); got != f.source {
		t.Fatal("source was not published")
	}
}

func (f releaseFixture) snapshot(t *testing.T) string {
	t.Helper()
	snapshot := mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD") +
		mustRun(t, f.repo, nil, "git", "status", "--porcelain", "--untracked-files=all") +
		mustRun(t, f.repo, nil, "git", "show-ref") +
		string(read(t, filepath.Join(f.repo, ".git/index")))
	err := filepath.WalkDir(f.repo, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == filepath.Join(f.repo, ".git") {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			snapshot += path + info.Mode().String() + string(read(t, path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestReleaseRejectsLockAndModulePathMismatch(t *testing.T) {
	for _, mode := range []string{"lock", "module-path"} {
		t.Run(mode, func(t *testing.T) {
			// Arrange.
			f := newReleaseFixture(t)
			if mode == "lock" {
				if err := os.MkdirAll(filepath.Join(f.repo, ".git/library-releases/lock"), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				write(
					t,
					filepath.Join(f.repo, "packages/test/go.mod"),
					[]byte("module example.invalid/wrong\n\ngo 1.27.1\n"),
				)
				mustRun(t, f.repo, nil, "git", "add", "packages/test/go.mod")
				mustRun(t, f.repo, nil, "git", "commit", "-qm", "wrong module path")
				f.source = mustRun(t, f.repo, nil, "git", "rev-parse", "HEAD")
			}
			before := f.snapshot(t)
			// Act.
			out, err := f.invoke(t, "patch", f.source)
			// Assert.
			if err == nil {
				t.Fatalf("unsafe release succeeded: %s", out)
			}
			if f.snapshot(t) != before ||
				mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote) != f.initialRefs {
				t.Fatal("rejected release changed caller or remote")
			}
		})
	}
}

func TestReleaseRejectsRemoteConflictDuringRecovery(t *testing.T) {
	// Arrange: retain the candidate, then reserve its root tag for a different object.
	f := newReleaseFixture(t)
	hook := filepath.Join(f.remote, "hooks/pre-receive")
	write(t, hook, []byte("#!/bin/sh\nexit 1\n"))
	if err := os.Chmod(hook, 0o700); err != nil {
		t.Fatal(err)
	}
	if out, err := f.invoke(t, "patch", f.source); err == nil {
		t.Fatal(out)
	}
	record := filepath.Join(f.repo, ".git/library-releases/active")
	candidate := string(read(t, filepath.Join(record, "candidate")))
	baseline := strings.Fields(f.initialRefs)[0]
	mustRun(t, f.repo, nil, "git", "--git-dir="+f.remote, "update-ref", "refs/tags/v0.0.1", baseline)
	before := mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote)
	caller := f.snapshot(t)
	// Act.
	if out, err := f.invoke(t, "inspect"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	out, err := f.invoke(t, "resume")
	// Assert: collision blocks publication and preserves the candidate and existing refs.
	if err == nil {
		t.Fatalf("remote conflict accepted: %s", out)
	}
	if strings.TrimSpace(string(read(t, filepath.Join(record, "status")))) != "collision" ||
		string(read(t, filepath.Join(record, "candidate"))) != candidate {
		t.Fatal("conflict or candidate identity changed")
	}
	if f.snapshot(t) != caller || mustRun(t, f.repo, nil, "git", "ls-remote", "--refs", f.remote) != before {
		t.Fatal("recovery changed caller or remote")
	}
}
