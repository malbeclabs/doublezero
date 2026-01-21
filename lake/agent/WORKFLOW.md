# Analysis Workflow

This document describes the deliberate, staged workflow for answering data questions. The goal is producing answers that are defensible, reproducible, and honest about their limits.

## Philosophy

When receiving a question, treat it less as "answer this" and more as "construct a defensible explanation from observed data." The workflow is deliberately staged to avoid jumping to conclusions or letting tooling drive the answer.

## Stages

### 1. Interpret the Question (Problem Framing)

Clarify what is actually being asked, not what it sounds like on the surface.

- What decision or curiosity is motivating this question?
- Is the question descriptive ("what happened?"), comparative ("A vs B"), diagnostic ("why did X change?"), or predictive ("what's likely next?")?
- What entities, time windows, and granularity are implied but unstated?
- What would a wrong answer look like, and how might I accidentally produce one?

Rewrite the question in one or two precise analytical forms before proceeding.

### 2. Map the Question to Data Reality

Translate the analytical question into concrete data terms.

- Which tables/views are authoritative for this?
- What is the unit of analysis (event, validator, link, time bucket, snapshot)?
- What joins are required, and are they safe (cardinality, time alignment, snapshot semantics)?
- Are there known gaps, delays, or quality caveats that could invalidate conclusions?

If the data required does not exist or is only partially available, stop and say so explicitly.

### 3. Plan the Minimum Viable Queries

Before running anything, plan the queries needed.

- Start with the smallest queries that validate assumptions (row counts, time coverage, null rates).
- Separate "exploration" queries from "answer-producing" queries.
- Decide what must be filtered early (time, status flags) vs derived later.
- Think about counterfactuals: what comparison or baseline will make the result interpretable?

This avoids expensive or misleading "one giant query."

### 4. Execute and Inspect

When running queries, don't trust the first result.

- Check row counts against intuition.
- Look for skew, outliers, or suspiciously clean results.
- Slice the data a second way to confirm the same pattern holds.
- If aggregating, spot-check raw rows underneath the aggregate.

If results contradict expectations, pause and explain why before proceeding.

### 5. Iterate Until the Signal is Stable

Most good answers require at least one refinement loop.

- Adjust filters or grouping after seeing real distributions.
- Add or remove joins once their impact is visible.
- Validate that derived metrics mean what the question assumes they mean.
- Explicitly rule out alternative explanations when possible.

Only once the pattern is robust should it be considered an "answer."

### 6. Translate Results into a Grounded Narrative

Turn data into an explanation.

- State what the data shows, not what it implies.
- Tie each claim directly to an observed metric or comparison.
- Quantify uncertainty, edge cases, or blind spots.
- Avoid causal language unless the data and design truly support it.

If the data is inconclusive, say so and explain what additional data would be needed.

### 7. Sanity-Check the Answer Against Reality

Before delivering the answer, verify:

- Does this align with known system behavior?
- Would a domain expert find this surprising, and if so, why?
- Is there a simpler explanation that's been ruled out?
- Could someone reproduce this result from the description?

Only then present the conclusion.

## Summary

The workflow is: **clarify → map to data → plan → validate → iterate → explain → sanity-check**.

The goal isn't speed or cleverness—it's producing answers that are defensible, reproducible, and honest about their limits.
