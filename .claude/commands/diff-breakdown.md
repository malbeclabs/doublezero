Analyze the diff between the current branch and origin/main and produce a categorized breakdown.

Steps:
1. Run `scripts/diff-breakdown.sh` to get the automated categorization. The script outputs JSON with:
   - `categories`: tallies (files, added, removed) for tests, fixtures, config, docs, generated, and unclassified
   - `unclassified_files`: list of files not auto-categorized, with per-file added/removed counts
   - `table`: a pre-formatted markdown table (use as a starting point)
   - `total`: aggregate totals
2. For each file in `unclassified_files`, read the diff (`git diff origin/main...HEAD -- <file>`) and classify as either:
   - **Scaffolding**: code that wires things together but contains little logic of its own:
     - Metrics/instrumentation definitions (`metrics.go`, prometheus boilerplate)
     - Thin CLI wrappers — files that are mostly clap/cobra struct definitions + a single function call (e.g. `enable.rs`, `disable.rs` that just call one controller method)
     - Subcommand/route registration (`mod.rs` adding a `pub mod`, `command.rs` adding a variant, `main.go` wiring a new dependency)
     - Interface/trait definitions that are pure signatures with no logic
   - **Core logic**: everything else — the files where the real business logic and algorithms live
3. Rebuild the table replacing "Unclassified" with the Scaffolding and Core logic rows. Omit categories with zero changes.

Output the breakdown as plain text (NOT inside a code block) so it's readable in the terminal. Use this format:

## Diff Breakdown (origin/main...HEAD)

| Category          | Files | Lines (+/-) | Net    |
|-------------------|-------|-------------|--------|
| Core logic        |     4 | +680 / -30  |   +650 |
| Scaffolding       |     5 | +200 / -10  |   +190 |
| Tests             |     3 | +820 / -10  |   +810 |
| Fixtures          |     7 | +14 / -14   |      0 |
| Config/build      |     1 | +2 / -0     |     +2 |
| **Total**         |    20 | +1716 / -64 |  +1652 |

### Core changes
- `client/doublezerod/internal/reconciler/reconciler.go` — +396/-0 (reconciliation loop, onchain state polling)
- `client/doublezero/src/command/status.rs` — +95/-12 (v2 status endpoint, synthetic disconnected entry)

Summary: ~680 lines of core logic across 4 files, supported by ~200 lines of scaffolding, ~820 lines of tests, and 7 fixture updates.

Guidelines:
- For the "Core changes" section, list only core logic files, sorted by lines changed (descending). Include a brief parenthetical note about what changed in each file (read the diff to understand).
- If there are more than 8 core logic files, show the top 8 and add a "... and N more files" line.
- The summary line should give a quick characterization of the branch's substance — how much is core logic vs supporting changes.
- Round line counts in the summary (use ~ prefix) for readability.
- When classifying scaffolding vs core logic, read the diff to understand the file's content. A file with real conditional logic, state management, or algorithms is core logic even if it's small. A file that's mostly declarations, registrations, or one-liner delegations is scaffolding.
