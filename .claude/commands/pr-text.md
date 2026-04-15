Generate a PR description for the current branch.

Analyze the **net changes** between the current branch and origin/main by examining:
1. Run `scripts/diff-breakdown.sh` to get the automated categorization (see its JSON output for category tallies, unclassified files, and a pre-formatted table)
2. The actual changes: `git diff origin/main...HEAD` (focus on key changes, not every line)

For each file in the script's `unclassified_files`, read the diff and classify as Scaffolding (wiring, metrics, thin CLI wrappers, registrations, interface-only) or Core logic (business logic, algorithms, state management).

IMPORTANT: Focus on what the branch adds/changes compared to origin/main as a whole. Do NOT describe individual commits or intermediate work. The reviewer only sees the final diff - they don't care about bugs introduced and fixed within the same branch.

Then generate a PR title and description. Output as a markdown code block on its own line (no text before the opening ```) so the user can easily copy it:

```markdown
# PR Title
<component>: <short description>

Resolves: #<issue number if known, otherwise omit this line>

## Summary of Changes
-
-

## Diff Breakdown
| Category     | Files | Lines (+/-) | Net  |
|--------------|-------|-------------|------|
| Core logic   |     X | +N / -N     |  +N  |
| Scaffolding  |     X | +N / -N     |  +N  |
| Tests        |     X | +N / -N     |  +N  |
| ...          |       |             |      |

<one-line summary>

<details>
<summary>Key files (click to expand)</summary>

- [`path/to/file.go`](PR_FILES_URL#diff-HASH) — brief description of what changed
- [`path/to/other.rs`](PR_FILES_URL#diff-HASH) — brief description of what changed

</details>

## Testing Verification
-
-
```

PR Title guidelines:
- Format: `<component>: <short description>` (e.g., "lake/indexer: add ClickHouse analytics service", "telemetry: fix metrics collection")
- Component should be the primary directory/module being changed
- Keep the description short and lowercase (except proper nouns)

Guidelines:
- Summary should describe the net result: what does this branch add or change compared to origin/main?
- Ignore commit history - only describe what the final diff shows
- Include a Diff Breakdown table categorizing changes (use the script output as a base, replacing Unclassified with Scaffolding and Core logic rows). Omit categories with zero changes. Add a one-line summary below the table characterizing the balance of changes.
- Include a "Key files" list after the diff breakdown showing the most important core logic files (up to 8), sorted by lines changed descending. Each entry should have a brief description of what changed. This helps reviewers know where to focus.
- Link each key file to its diff in the PR using the `pr_url` and `diff_hash` fields from the script output: `<pr_url>/files#diff-<diff_hash>`. If no PR exists yet (`pr_url` is empty), use plain backtick paths instead.
- Testing Verification should describe how the changes were tested (e.g., unit tests added/passing, manual testing performed, build verified)
- Focus on the "what" and "why", not the "how"
- Group related changes together
- Mention any breaking changes or migration steps if applicable
