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

## Summary of Changes
-
-

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
- Testing Verification should describe how the changes were tested (e.g., unit tests added/passing, manual testing performed, build verified)
- Focus on the "what" and "why", not the "how"
- Group related changes together
- Mention any breaking changes or migration steps if applicable
