#!/bin/bash
set -euo pipefail

fail() { echo "Error: $*" >&2; exit 1; }
release_type=${1:-}
modules=${2:-}
[[ "$release_type" == patch || "$release_type" == break ]] || fail 'Expected patch or break.'
[[ "$modules" == . ]] || fail 'Only the root module (MODULES=.) is supported.'
[[ -f go.mod ]] || fail 'Run from the repository root.'
[[ "$(git rev-parse --show-prefix)" == '' ]] || fail 'Run from the repository root.'
head=$(git rev-parse --verify HEAD)
git diff --quiet -- || fail 'Tracked worktree changes must be committed first.'
git diff --cached --quiet -- || fail 'Staged changes must be committed first.'
destination=$(git remote get-url --push origin)
# Freeze relative filesystem remotes before changing to the preparation clone.
case "$destination" in
    /*|*:*|*://*) ;;
    *) destination="$(pwd)/$destination" ;;
esac
remote_tags=$(git ls-remote --tags --refs "$destination") || fail 'Cannot inspect published tags.'
major=0; minor=0; patch=0
while read -r oid ref; do
    if [[ "$ref" =~ ^refs/tags/v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
        # Avoid ambiguous leading zeroes and shell arithmetic overflow.
        a=${BASH_REMATCH[1]}; b=${BASH_REMATCH[2]}; c=${BASH_REMATCH[3]}
        for component in "$a" "$b" "$c"; do
            [[ "$component" =~ ^(0|[1-9][0-9]{0,8})$ ]] || fail 'Unsupported version component.'
        done
        if (( a > major || (a == major && b > minor) || (a == major && b == minor && c > patch) )); then
            major=$a; minor=$b; patch=$c
        fi
    fi
done <<< "$remote_tags"
root_module=$(awk '$1 == "module" {print $2; exit}' go.mod)
[[ -n "$root_module" ]] || fail 'Missing module path.'
if (( major >= 2 )); then
    [[ "$root_module" == */v"$major" ]] || fail 'Published major does not match semantic import path.'
fi
latest="v$major.$minor.$patch"
if [[ "$release_type" == break ]]; then
    (( major == 0 )) || fail 'Major break requires a reviewed semantic import-version change.'
    minor=$((minor + 1)); patch=0
else
    patch=$((patch + 1))
fi
version="v$major.$minor.$patch"
echo "Release $latest -> $version at $head"
read -r -p "Publish $version? [y/N] " reply || fail 'Confirmation unavailable.'
[[ "$reply" == y || "$reply" == Y ]] || fail 'Aborted.'

prepared=$(mktemp -d "${TMPDIR:-/tmp}/memy-release.XXXXXXXX")
keep=false
cleanup() {
    if [[ "$keep" == false ]]; then rm -rf -- "$prepared"; fi
}
trap cleanup EXIT
# --no-local prevents alternates/hardlinks; --no-tags excludes unrelated local refs.
git clone --quiet --no-local --no-tags --no-checkout "$(pwd)" "$prepared/repo"
git -C "$prepared/repo" checkout --quiet --detach "$head"
git -C "$prepared/repo" -c tag.gpgSign=false tag "$version" "$head"
echo "State: local-prepared; tag=$version commit=$head"
if git -C "$prepared/repo" push "$destination" "refs/tags/$version:refs/tags/$version"; then
    echo "State: published; tag=$version commit=$head"
    exit 0
fi
# A transport failure need not mean the remote transaction failed.
if observed=$(git ls-remote --tags --refs "$destination" "refs/tags/$version"); then
    if [[ "$observed" == "$head"$'\t'"refs/tags/$version" ]]; then
        echo "State: published; tag=$version commit=$head (confirmed after push failure)"
        exit 0
    fi
    echo "State: rejected; tag=$version commit=$head" >&2
    exit 1
fi
keep=true
echo "State: unknown; tag=$version commit=$head; recovery=$prepared/repo" >&2
exit 1
