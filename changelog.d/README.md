# changelog.d

Each PR that needs a changelog entry adds one new file here instead of editing
`CHANGELOG.md`. Two PRs then touch different files, so their entries cannot conflict.

The testnet release bump (`scripts/release/bump-version.sh`) folds every fragment
into `## Unreleased` in `CHANGELOG.md`, deletes the fragments, and then promotes
the section to the new version. The `changelog-reminder` check accepts a new
fragment in place of a `CHANGELOG.md` change.

## File name

Name the file after your branch, with `/` replaced by `-`: branch
`alice/fix-heartbeat` writes `changelog.d/alice-fix-heartbeat.md`. Any name
that ends in `.md` works, except `README.md`. Fragments are merged in file name
order.

## Format

A fragment holds the lines you would add under `## Unreleased`: a `### Breaking`
or `### Changes` heading, then a `- Component` line with `  - entry` sub-bullets.
Indent continuation lines by four spaces. A fragment can hold both headings.

```markdown
### Breaking

- SDK
  - `CreateUserCommand` takes `accesspass_pk`. Code that builds it field by field must set it.

### Changes

- CLI
  - `doublezero keygen --import` also accepts a Solana keypair JSON array.
```

Use a component name that `CHANGELOG.md` already uses, such as CLI, Client,
Serviceability, SDK, Telemetry or E2E/QA. Names that differ only in case merge,
and the first spelling wins.

Any other line makes the bump fail before it changes a file. Check your fragment
with:

```bash
python3 scripts/release/collate_changelog.py --dry-run
```

## Entries merged after the bump PR

The bump PR folds only the fragments on its branch. A fragment that merges after
the bump branch is cut stays in `changelog.d/` and goes into the next release, even
if the tag contains the change. To credit it to the tagged release, move its entry
into that version's section of `CHANGELOG.md` by hand and delete the fragment in
the same PR.

The per-crate changelogs under `offchain/` do not use fragments. Edit them directly.
