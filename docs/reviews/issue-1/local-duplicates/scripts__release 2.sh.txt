#!/bin/sh
set -eu

release_version=${1:?Usage: release.sh vMAJOR.MINOR.PATCH}
if ! printf '%s\n' "$release_version" | grep -Eq '^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'; then
    echo 'Release version must use vMAJOR.MINOR.PATCH without leading zeroes.' >&2
    exit 1
fi
if [ -n "$(git status --porcelain)" ]; then
    echo 'Commit all changes before releasing.' >&2
    exit 1
fi
if [ "$(git branch --show-current)" != main ]; then
    echo 'Release from main.' >&2
    exit 1
fi

release_repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
if [ "$release_repo" != skosovsky/memy ]; then
    echo 'Release remote must point to skosovsky/memy.' >&2
    exit 1
fi
release_commit=$(git rev-parse HEAD)
remote_tags=$(git ls-remote --tags origin "refs/tags/$release_version" "refs/tags/$release_version^{}")
remote_commit=$(printf '%s\n' "$remote_tags" | awk '/\^\{\}$/ {peeled=$1; next} NF {tag=$1} END {print peeled ? peeled : tag}')
if [ -n "$remote_commit" ] && [ "$remote_commit" != "$release_commit" ]; then
    echo 'Remote release tag belongs to another commit.' >&2
    exit 1
fi
if git show-ref --verify --quiet "refs/tags/$release_version"; then
    if [ "$(git rev-parse "$release_version^{commit}")" != "$release_commit" ]; then
        echo 'Local release tag belongs to another commit.' >&2
        exit 1
    fi
else
    git tag -a "$release_version" -m "memy $release_version"
fi

git push --atomic origin HEAD:refs/heads/main "refs/tags/$release_version"
if gh release view "$release_version" --repo "$release_repo" >/dev/null 2>&1; then
    gh release view "$release_version" --repo "$release_repo" --json url --jq .url
else
    gh release create "$release_version" --repo "$release_repo" --verify-tag \
        --title "$release_version" --generate-notes
fi
