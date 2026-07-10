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

# cargo update -w re-resolves the members' dependency edges too, and a
# requirement that admits multiple locked majors can silently rebind them
# (observed: solana-system-interface 3.2.0 -> 2.0.0 under ">=1,<=3"). A bump
# may only ADD the members' new version lines — any other added line is a
# rebind/upgrade/downgrade and means a poisoned bump PR, so fail loudly
# instead of opening it. Pure removals are cargo pruning stale lock entries
# (legitimate normalization); log them for the PR reviewer.
NEW_RE=${NEW//./\\.}
PREV_RE=${PREV//./\\.}
BAD_LINES=$(git diff -U0 -- Cargo.lock | grep -E '^\+[^+]' | grep -vE "^\+version = \"$NEW_RE\"\$" || true)
if [ -n "$BAD_LINES" ]; then
  echo "cargo update --workspace changed more than member versions in Cargo.lock:" >&2
  echo "$BAD_LINES" >&2
  exit 1
fi
PRUNED=$(git diff -U0 -- Cargo.lock | grep -E '^-[^-]' | grep -vE "^-version = \"$PREV_RE\"\$" || true)
[ -z "$PRUNED" ] || { echo "note: cargo update pruned stale lock entries (review, but expected):"; echo "$PRUNED"; }

grep -q '^## Unreleased$' CHANGELOG.md || { echo "CHANGELOG.md has no '## Unreleased' section" >&2; exit 1; }
BODY=$(awk '/^## Unreleased$/{f=1; next} /^## /{f=0} f' CHANGELOG.md | grep -v '^###' | grep -cv '^[[:space:]]*$' || true)
[ "$BODY" -gt 0 ] || echo "warning: '## Unreleased' has no entries; promoting an empty v$NEW section" >&2
DATE=$(date -u +%Y-%m-%d)
export NEW PREV DATE
perl -0pi -e 's{^## Unreleased\n}{## Unreleased\n\n### Breaking\n\n### Changes\n\n## [v$ENV{NEW}](https://github.com/malbeclabs/doublezero/compare/client/v$ENV{PREV}...client/v$ENV{NEW}) - $ENV{DATE}\n}m' CHANGELOG.md

echo "Bumped $PREV -> $NEW"
git --no-pager diff --stat -- Cargo.toml Cargo.lock CHANGELOG.md
