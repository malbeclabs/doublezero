#!/usr/bin/env bash
# Bumps the workspace version and promotes the CHANGELOG "Unreleased" section
# for a testnet release. Run from the repo root. Requires cargo on PATH.
set -euo pipefail

NEW="${1:?usage: bump-version.sh X.Y.Z}"
[[ "$NEW" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "version must be X.Y.Z" >&2; exit 1; }

PREV=$(grep -m1 '^version = ' Cargo.toml | sed 's/version = "\(.*\)"/\1/')
[ "$PREV" != "$NEW" ] || { echo "workspace is already at $NEW" >&2; exit 1; }
COUNT=$(grep -c "^version = \"$PREV\"\$" Cargo.toml)
[ "$COUNT" -eq 1 ] || { echo "expected exactly one workspace version line, found $COUNT" >&2; exit 1; }

sed -i "s/^version = \"$PREV\"\$/version = \"$NEW\"/" Cargo.toml
cargo update --workspace

grep -q '^## Unreleased$' CHANGELOG.md || { echo "CHANGELOG.md has no '## Unreleased' section" >&2; exit 1; }
BODY=$(awk '/^## Unreleased$/{f=1; next} /^## /{f=0} f' CHANGELOG.md | grep -v '^###' | grep -cv '^[[:space:]]*$' || true)
[ "$BODY" -gt 0 ] || echo "warning: '## Unreleased' has no entries; promoting an empty v$NEW section" >&2
DATE=$(date -u +%Y-%m-%d)
export NEW PREV DATE
perl -0pi -e 's{^## Unreleased\n}{## Unreleased\n\n### Breaking\n\n### Changes\n\n## [v$ENV{NEW}](https://github.com/malbeclabs/doublezero/compare/client/v$ENV{PREV}...client/v$ENV{NEW}) - $ENV{DATE}\n}m' CHANGELOG.md

echo "Bumped $PREV -> $NEW"
git --no-pager diff --stat -- Cargo.toml Cargo.lock CHANGELOG.md
