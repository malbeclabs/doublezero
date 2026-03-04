Generate a PR description for the current branch.

Analyze the **net changes** between the current branch and origin/main by examining:
1. First, run `git fetch origin` to ensure remote tracking is up to date
2. The diff summary: `git diff origin/main...HEAD --stat`
3. The actual changes: `git diff origin/main...HEAD` (focus on key changes, not every line)

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
- Include a Diff Breakdown table categorizing changes using `git diff origin/main...HEAD --numstat`. Categorize files as: Core logic, Scaffolding (metrics, thin CLI wrappers, subcommand registration, interface-only files), Tests, Fixtures, Config/build, Docs, Generated. Omit categories with zero changes. Add a one-line summary below the table characterizing the balance of changes.
- Include a "Key files" list after the diff breakdown showing the most important core logic files (up to 8), sorted by lines changed descending. Each entry should have a brief description of what changed. This helps reviewers know where to focus.
- Link each key file to its diff in the PR. Use `gh pr view --json number,url` to get the PR URL, then link to `<PR_URL>/files#diff-<SHA256_OF_FILE_PATH>` where the hash is `echo -n "path/to/file" | shasum -a 256`. If no PR exists yet, use plain backtick paths instead.
- Testing Verification should describe how the changes were tested (e.g., unit tests added/passing, manual testing performed, build verified)
- Focus on the "what" and "why", not the "how"
- Group related changes together
- Mention any breaking changes or migration steps if applicable
