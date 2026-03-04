Generate a CHANGELOG.md entry for the current branch.

Analyze the **net changes** between the current branch and origin/main by examining:
1. First, run `git fetch origin` to ensure remote tracking is up to date
2. The diff summary: `git diff origin/main...HEAD --stat`
3. The actual changes: `git diff origin/main...HEAD` (focus on key changes, not every line)

IMPORTANT: Focus on what the branch adds/changes compared to origin/main as a whole. Do NOT describe individual commits or intermediate work. The reviewer only sees the final diff.

Then read the existing CHANGELOG.md at the repo root to understand the format. Add new entries to the existing `## Unreleased` section under `### Changes`.

The CHANGELOG uses this format — entries are grouped by component with sub-bullets for details:

```markdown
## Unreleased

### Breaking

- None for this release

### Changes

- Component Name
  - Description of change
  - Another change
- Another Component
  - Description of change
```

Common component names used in this project: CLI, Client, Onchain programs, Smartcontract, SDK, Telemetry, Activator, Device controller, Device agents, E2E tests, CI, Monitor, Funder, Tools, RFCs.

Guidelines:
- Add entries under the existing `## Unreleased` / `### Changes` section, do not create a new section
- Group entries by component, matching existing component names in the CHANGELOG
- Each entry should be a concise description of the change
- Focus on the "what" and "why", not the "how"
- Group related changes into single entries rather than listing every file touched
- Use present tense (e.g., "Add support for..." not "Added support for...")
