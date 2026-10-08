#!/bin/bash
# Portable to the system Bash 3.2 on macOS.
set -euo pipefail
export GOWORK=off
fail() { echo "Error: $*" >&2; exit 1; }
repo_lock=''; state_lock=''; verification_pid=''; watchdog_pid=''
cleanup() {
    [[ -z "$verification_pid" ]] || kill -TERM -- "-$verification_pid" 2>/dev/null || true
    [[ -z "$watchdog_pid" ]] || kill -TERM -- "-$watchdog_pid" 2>/dev/null || true
    [[ -z "$state_lock" ]] || rmdir "$state_lock"
    [[ -z "$repo_lock" ]] || rmdir "$repo_lock"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
put() {
    printf '%s\n' "$2" > "$state/$1.tmp"
    mv "$state/$1.tmp" "$state/$1"
}
get() {
    [[ -f "$state/$1" && ! -L "$state/$1" ]] || fail "Invalid recovery field $1; use previous tooling for legacy records."
    value=$(cat "$state/$1")
    [[ -n "$value" && "$value" != *$'\n'* && "$value" != *$'\r'* ]] || fail "Invalid field $1."
    printf '%s' "$value"
}
lock_repo() {
    mkdir "$1/memy-release.lock" 2>/dev/null || fail "Repository release lock exists: $1/memy-release.lock; inspect before removing a stale lock."
    repo_lock="$1/memy-release.lock"
}
lock_record() {
    mkdir "$state/lock" 2>/dev/null || fail "Recovery lock exists: $state/lock; inspect before removing a stale lock."
    state_lock="$state/lock"
}
load() {
    [[ -d "$state" ]] || fail 'Recovery directory does not exist; use previous tooling for legacy records.'
    [[ "$(get format)" == 1 ]] || fail 'Unsupported record; use previous tooling.'
    source=$(get source); candidate=$(get candidate); version=$(get version)
    destination=$(get destination); ref=$(get ref); phase=$(get phase)
    original_git=$(get original-git)
    export GOLANGCI_LINT="${GOLANGCI_LINT:-$(get linter)}"
    [[ "$source" =~ ^[0-9a-f]{40}$ && "$candidate" == "$source" ]] || fail 'Invalid source/candidate identity.'
    [[ "$version" =~ ^v(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})\.(0|[1-9][0-9]{0,8})$ ]] || fail 'Invalid version.'
    [[ "$ref" == "refs/tags/$version" && "$destination" != -* ]] || fail 'Invalid destination/ref.'
    case "$phase" in prepared|publishing|published-unverified|complete|conflict|unknown) ;; *) fail 'Invalid phase.' ;; esac
    [[ "$original_git" == /* && -d "$original_git" ]] || fail 'Original repository unavailable.'
    [[ "$(git -C "$state/repo" rev-parse --verify HEAD)" == "$candidate" ]] || fail 'Candidate checkout identity changed.'
    git -C "$state/repo" diff --quiet && git -C "$state/repo" diff --cached --quiet || fail 'Candidate modified.'
    [[ "$(git -C "$state/repo" rev-parse --verify "$ref")" == "$candidate" ]] || fail 'Prepared tag identity changed.'
}
observe() {
    if ! observed=$(git ls-remote --tags --refs "$destination" "$ref"); then return 2; fi
    if [[ "$observed" == "$candidate"$'\t'"$ref" ]]; then return 0; fi
    [[ -z "$observed" ]] && return 1
    return 3
}
bounded_public_check() {
    # Bash job control gives this verification its own process group on both OSes.
    # The watchdog bounds Make/Go startup as well as the Go test itself.
    set -m
    (cd "$state/repo" && MEMY_PUBLIC_TIMEOUT=110s exec make --no-print-directory test-published MEMY_REF="$version" TEST_TIMEOUT=115s) &
    verification_pid=$!
    (sleep "$1"; kill -KILL -- "-$verification_pid" 2>/dev/null || true) &
    watchdog_pid=$!
    set +m
    result=0
    wait "$verification_pid" || result=$?
    kill -TERM -- "-$watchdog_pid" 2>/dev/null || true
    wait "$watchdog_pid" 2>/dev/null || true
    verification_pid=''; watchdog_pid=''
    return "$result"
}
verify_public() {
    put phase published-unverified
    attempt=1; retry_delay=3
    started=$SECONDS
    while (( attempt <= 6 && SECONDS - started < 300 )); do
        echo "Public verification attempt $attempt/6: $version"
        remaining=$((300 - SECONDS + started)); budget=120
        (( remaining >= budget )) || budget=$remaining
        if bounded_public_check "$budget"; then
            if observe; then
                put phase complete
                echo "State: complete; tag=$version commit=$candidate; recovery=$state"
                return 0
            fi
            put phase unknown
            fail 'Remote identity unavailable or changed after public verification.'
        fi
        attempt=$((attempt + 1))
        if (( attempt <= 6 && SECONDS - started + retry_delay < 300 )); then
            sleep "$retry_delay"; retry_delay=$((retry_delay * 2))
        else break; fi
    done
    fail "Public verification incomplete; state=published-unverified; run finish $state"
}
check_candidate() {
    (cd "$state/repo" && make --no-print-directory check)
    (cd "$state/repo" && make --no-print-directory release-candidate RELEASE_SOURCE="$source" RELEASE_VERSION="$version")
    [[ "$(git -C "$state/repo" rev-parse HEAD)" == "$candidate" ]] || fail 'Candidate changed during checks.'
    git -C "$state/repo" diff --quiet && git -C "$state/repo" diff --cached --quiet || fail 'Checks modified candidate.'
}
publish() {
    echo "Source: $source"
    echo "Candidate: $candidate"
    echo "Version: $version"
    echo "Destination: $destination"
    echo "Refs: $ref -> $candidate"
    read -r -p 'Publish these exact refs? [y/N] ' reply || fail 'Confirmation unavailable.'
    [[ "$reply" == y || "$reply" == Y ]] || fail "Aborted; prepared recovery=$state"
    # Reconcile immediately before publication, including a prior lost response.
    if observe; then verify_public; return; else result=$?; fi
    case "$result" in
        1) ;;
        2) put phase unknown; fail 'Remote unavailable before push.' ;;
        *) put phase conflict; fail 'Remote tag identity conflicts with candidate.' ;;
    esac
    put phase publishing
    if git -C "$state/repo" push --atomic "$destination" "$ref:$ref"; then
        if observe; then verify_public; return; else result=$?; fi
    else
        if observe; then
            echo 'Remote publication confirmed after push failure.'
            verify_public; return
        else result=$?; fi
    fi
    case "$result" in
        1) put phase prepared; fail "Push rejected; same candidate retained at $state" ;;
        2) put phase unknown; fail "Push outcome unknown; recovery=$state" ;;
        *) put phase conflict; fail "Remote tag conflicts; recovery=$state" ;;
    esac
}
operation=${1:-}
case "$operation" in
    inspect|resume|finish)
        [[ $# == 2 ]] || fail 'Expected operation and recovery directory.'
        state=$(cd "$2" && pwd) || fail 'Recovery directory unavailable.'
        load
        echo "Recovery: $state; state=$phase; version=$version; source=$source; candidate=$candidate; destination=$destination; ref=$ref"
        if [[ "$operation" == inspect ]]; then
            if observe; then echo 'Remote: exact candidate'; else result=$?; case "$result" in 1) echo 'Remote: absent';; 2) fail 'Remote unavailable';; *) fail 'Remote conflict';; esac; fi
            exit 0
        fi
        lock_repo "$original_git"; lock_record
        if observe; then verify_public; exit 0; else result=$?; fi
        case "$result" in 1) ;; 2) put phase unknown; fail 'Remote unavailable';; *) put phase conflict; fail 'Remote conflict';; esac
        [[ "$phase" != complete ]] || fail "Completed release tag disappeared; manual reconciliation required."
        [[ "$operation" == resume ]] || fail 'finish never pushes; remote tag is absent.'
        check_candidate
        publish
        exit 0
        ;;
    patch|break) ;;
    *) fail 'Expected patch, break, inspect, resume or finish.' ;;
esac
[[ $# == 1 || ( $# == 2 && "$2" == . ) ]] || fail 'Only the root publishable module is supported.'
[[ -f go.mod && "$(git rev-parse --show-prefix)" == '' ]] || fail 'Run from repository root.'
git diff --quiet && git diff --cached --quiet || fail 'Tracked worktree/index changes must be committed first.'
export GOLANGCI_LINT="${GOLANGCI_LINT:-$(pwd)/bin/golangci-lint}"
case "$GOLANGCI_LINT" in /*) ;; */*) GOLANGCI_LINT="$(pwd)/$GOLANGCI_LINT" ;; *) GOLANGCI_LINT=$(command -v "$GOLANGCI_LINT") || fail "Pinned linter unavailable." ;; esac
source=$(git rev-parse --verify "${RELEASE_SOURCE:-HEAD}^{commit}")
candidate=$source
original_git=$(cd "$(git rev-parse --git-common-dir)" && pwd)
lock_repo "$original_git"
destination=$(git remote get-url --push --all origin)
[[ -n "$destination" && "$destination" != *$'\n'* && "$destination" != -* ]] || fail 'Exactly one push destination required.'
case "$destination" in /*|*:*|*://*) ;; *) destination="$(pwd)/$destination" ;; esac
remote_tags=$(git ls-remote --tags --refs "$destination") || fail 'Cannot inspect published tags.'
major=0; minor=0; patch=0
while read -r oid tag; do
    if [[ "$tag" =~ ^refs/tags/v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
        a=${BASH_REMATCH[1]}; b=${BASH_REMATCH[2]}; c=${BASH_REMATCH[3]}
        for component in "$a" "$b" "$c"; do
            [[ "$component" =~ ^(0|[1-9][0-9]{0,8})$ ]] || fail 'Unsupported version component.'
        done
        if (( a > major || (a == major && b > minor) || (a == major && b == minor && c > patch) )); then major=$a; minor=$b; patch=$c; fi
    fi
done <<< "$remote_tags"
root_module=$(git show "$source:go.mod" | awk '$1 == "module" {print $2; exit}')
[[ -n "$root_module" ]] || fail 'Missing module path.'
if (( major >= 2 )); then [[ "$root_module" == */v"$major" ]] || fail 'Published major does not match semantic import path.'; fi
if [[ "$operation" == break ]]; then
    (( major == 0 )) || fail 'Major break requires reviewed semantic import-version migration.'
    minor=$((minor + 1)); patch=0
else patch=$((patch + 1)); fi
for component in "$major" "$minor" "$patch"; do
    [[ "$component" =~ ^(0|[1-9][0-9]{0,8})$ ]] || fail "Next version exceeds supported numeric bounds."
done
version="v$major.$minor.$patch"
ref="refs/tags/$version"
state=$(mktemp -d "${TMPDIR:-/tmp}/memy-release.XXXXXXXX")
echo "Recovery: $state"
lock_record
# --no-local avoids alternates/hardlinks; no unrelated local refs are copied.
git clone --quiet --no-local --no-tags --no-checkout "$(pwd)" "$state/repo"
git -C "$state/repo" checkout --quiet --detach "$source"
[[ "$(cd "$state/repo" && make --no-print-directory print-publishable-modules)" == . ]] || fail 'Unsupported publishable inventory.'
git -C "$state/repo" -c tag.gpgSign=false tag "$version" "$candidate"
put format 1; put source "$source"; put candidate "$candidate"; put version "$version"
put destination "$destination"; put ref "$ref"; put original-git "$original_git"; put linter "$GOLANGCI_LINT"; put phase prepared
check_candidate
publish
