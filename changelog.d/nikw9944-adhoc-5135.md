### Changes

- CI
  - A PR adds its changelog entry as a new file in `changelog.d/` instead of an edit to `CHANGELOG.md`, so concurrent PRs no longer conflict on `## Unreleased`. `scripts/release/bump-version.sh` folds the fragments into the new version section and deletes them. `changelog-reminder` accepts a new fragment, and `release-bump-dry-run` fails on a fragment that does not parse.
