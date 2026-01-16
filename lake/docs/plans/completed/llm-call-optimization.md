# Plan: Reduce LLM Calls in Agent Pipeline

**Status**: ✅ Completed
**Date**: 2026-01-15

## Problem

The agent pipeline was making 12-30 LLM calls per user query. Based on analysis of eval runs, here's where calls were going:

| Stage | Calls | Notes |
|-------|-------|-------|
| CLASSIFY | 1 | Fixed |
| DECOMPOSE | 1 | Fixed |
| GENERATE | 3-6 | One per decomposed question |
| Zero-row analysis | 0-4 | 2 per suspicious zero-row result |
| Error retries | 0-4 | 1 per retry attempt |
| SYNTHESIZE | 1 | Fixed |

**Target**: Reduce average from ~19 calls to ~8-10 calls per query.

---

## Results

| Metric | Before | After | Change |
|--------|--------|-------|--------|
| Avg calls/analysis | 19.0 | 12.3 | **-35%** |
| Min calls | 12 | 8 | -33% |
| Max calls | 30 | 24 | -20% |

All evals continue to pass.

---

## Issue 1: Decomposition Generates Too Many Questions ✅ DONE

**Problem**: DECOMPOSE broke user questions into 3-6 sub-questions, each requiring a GENERATE call.

**Fix**: Updated `agent/pkg/pipeline/prompts/DECOMPOSE.md`:
- Added explicit guidance: "prefer 1-2 queries, maximum 3"
- Updated all examples to show consolidated single-query approaches
- Emphasized that synthesis step can count rows and sum columns

**Commit**: `64b6d356`
**Result**: Avg calls dropped from 17.7 → 13.7 (-23%)

---

## Issue 2: Zero-Row Analysis Triggers Too Often ✅ DONE

**Problem**: Every query returning 0 rows triggered analysis + regeneration, even for time-bounded queries where zero rows is legitimate.

**Fix**: Updated `agent/pkg/pipeline/generate.go` AnalyzeZeroResult prompt:
- Added guidance that time-bounded queries can legitimately return zero rows
- Still checks for real query bugs (incorrect JOINs, wrong column values)

**Commit**: `d97c36af`
**Result**: Avg calls dropped from 13.7 → 12.3 (-10%)

---

## Issue 3: Type Conversion Errors Cause Unnecessary Retries ✅ DONE

**Problem**: ClickHouse `Date` columns failed with type conversion errors, triggering LLM retries that couldn't fix the issue.

**Fix**: Added proper type handling in `indexer/pkg/clickhouse/dataset/scan.go`:
- Added `Date`, `Date32` → `time.Time`
- Added `UInt16`, `UInt32`, `Int8`, `Int16`, `Float32` for completeness

**Commit**: `2d1f09c1`
**Result**: Avg calls dropped from 19.0 → 17.7 (-7%)
