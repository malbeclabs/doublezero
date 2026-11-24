Generate a changelog fragment for the current branch.

Analyze the **net changes** between the current branch and origin/main by examining:
1. Run `scripts/diff-breakdown.sh` to get the automated categorization (see its JSON output for category tallies and unclassified files)
2. The actual changes: `git diff origin/main...HEAD` (focus on key changes, not every line)

IMPORTANT: Focus on what the branch adds/changes compared to origin/main as a whole. Do NOT describe individual commits or intermediate work. The reviewer only sees the final diff.

Do NOT edit `CHANGELOG.md`. Write the entry to a new file, `changelog.d/<branch>.md`, where `<branch>` is the current branch name with `/` replaced by `-`. If that file exists, update it. `changelog.d/README.md` describes the format, and the release bump folds the fragments into `CHANGELOG.md`. Read the recent sections of `CHANGELOG.md` to match the component names and style.

A fragment holds the lines that would go under `## Unreleased`: entries grouped by component, with sub-bullets for details. Include `### Breaking` only when the change breaks a caller:

```markdown
### Breaking

- SDK
  - Description of the breaking change

### Changes

- Component Name
  - Description of change
  - Another change
- Another Component
  - Description of change
```

Common component names used in this project: CLI, Client, Serviceability, SDK, Telemetry, Controller, Onchain programs, E2E/QA, CI, Activator, Tools, Monitor.

Guidelines:
- Group entries by component, matching existing component names in the CHANGELOG
- Indent continuation lines of an entry by four spaces; any other line shape fails the release bump
- Each entry should be a concise description of the change
- Focus on the "what" and "why", not the "how"
- Group related changes into single entries rather than listing every file touched
- Use present tense (e.g., "Add support for..." not "Added support for...")
- Check the result with `python3 scripts/release/collate_changelog.py --dry-run`
- The per-crate changelogs under `offchain/` do not use fragments; edit their `## [Unreleased]` sections directly
