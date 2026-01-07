# DoubleZero AI Prompts

Restructured prompt architecture optimized for LLM cognition.

---

## Architecture

```
prompts/
├── IDENTITY.md      # Who (primacy position)
├── CONSTRAINTS.md   # Hard rules (tiered)
├── WORKFLOW.md      # Process
├── CATALOG.md       # Data reference
├── FORMATTING.md    # Output style
├── EXAMPLES.md      # Good/bad patterns (recency position)
├── FINALIZATION.md  # End-of-turn
├── prompts.go       # Loader
└── embed.go         # Embed directive
```

### Why This Order?

LLMs have **primacy** (remember the beginning) and **recency** (remember the end) biases.

1. **IDENTITY** — First, so the model knows who it is
2. **CONSTRAINTS** — Hard rules up front, tiered by severity
3. **WORKFLOW** — Operating procedure
4. **CATALOG** — Reference material (middle = ok to skim)
5. **FORMATTING** — Output rules near the end
6. **EXAMPLES** — Last, so patterns are fresh when generating

---

## Key Changes from Original

### 1. Fixed the Review Loop Problem

**Before:** RESPOND → REVIEW → REVISE (impossible—LLMs can't delete output)

**After:** PLAN → EXECUTE → VERIFY → RESPOND (verification before output)

### 2. Consolidated Constraints

**Before:** 20+ "CRITICAL" markers scattered across 3 files

**After:** Single CONSTRAINTS.md with three tiers:

- ⛔ **Must** — Violations cause incorrect output
- ⚠️ **Should** — Strong defaults, override when requested
- 💡 **May** — Contextual guidelines

### 3. Removed Duplicate Sections

**Before:** Review Phase repeated everything from Response Generation

**After:** One place for each concept, cross-referenced

### 4. Added Concrete Examples

**Before:** No examples of good vs bad responses

**After:** EXAMPLES.md with patterns for all common query types

### 5. Code Blocks for Dense Data

**Before:** "Use lists" (hard to compare metrics)

**After:** Explicit guidance to use aligned code blocks:

```text
LINK          LOSS    RTT
tok-fra-1     0.0%    24ms
nyc-lon-2     1.2%    68ms
```

### 6. Removed "Alternative" Patterns

**Before:** Preferred view + Alternative CTE (choice = error)

**After:** Just the view. No choice to make.

---

## Token Comparison

| File           | Original | New    | Change   |
| -------------- | -------- | ------ | -------- |
| ROLE.md        | ~4,200   | —      | Replaced |
| IDENTITY.md    | —        | ~100   | New      |
| CONSTRAINTS.md | —        | ~800   | New      |
| WORKFLOW.md    | —        | ~700   | New      |
| CATALOG.md     | ~4,800   | ~2,400 | -50%     |
| FORMATTING.md  | —        | ~700   | New      |
| EXAMPLES.md    | —        | ~1,200 | New      |
| SLACK.md       | ~450     | —      | Merged   |
| **Total**      | ~9,450   | ~5,900 | **-38%** |

Fewer tokens, better structure, more effective.

---

## Usage

```go
prompts, err := prompts.Load()
if err != nil {
    log.Fatal(err)
}

systemPrompt := prompts.BuildSystemPrompt()
// or
slackPrompt := prompts.BuildSlackSystemPrompt()
```

---

## Verification Tests

After deploying, test these scenarios:

1. **Network health** — Should produce code block with device/link codes
2. **Latency comparison** — Should include avg + p95, code block
3. **Solana validators** — Should use `solana_validator_dz_first_connection_events`
4. **Missing data** — Should explicitly state unavailability
5. **Follow-up "what about now?"** — Should re-query, not reuse stale data

---

## Files

### IDENTITY.md

Short statement of who the agent is. Goes first for primacy.

### CONSTRAINTS.md

All hard rules in one place, tiered by severity. No more scattered "CRITICAL" markers.

### WORKFLOW.md

The operating procedure: PLAN → EXECUTE → VERIFY → RESPOND.
Includes specific patterns for network health, incidents, Solana queries.

### CATALOG.md

Data schemas, views, and query patterns. Streamlined from original—removed duplicate constraint summaries and the "Alternative" CTE pattern.

### FORMATTING.md

Output style rules. Merged Slack-specific rules into general guidance since all output goes to Slack anyway.

### EXAMPLES.md

Concrete good/bad response patterns. LLMs learn from examples better than rules.

### FINALIZATION.md

End-of-turn instructions. Unchanged from original.
