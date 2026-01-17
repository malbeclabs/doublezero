# Agent Cost Optimization

This document summarizes the cost analysis and optimizations for the Lake agent workflow.

## Current Architecture

The agent uses a 4-step workflow for data analysis questions:

1. **CLASSIFY** - Route question (data_analysis / conversational / out_of_scope)
2. **DECOMPOSE** - Break question into specific data questions
3. **GENERATE** - Create SQL for each data question (parallel, with retries)
4. **SYNTHESIZE** - Combine query results into a cited answer

## Model Configuration

- **Production API:** `claude-3.5-haiku-20241022`
- **Evals:** `claude-haiku-4-5-20251001`

All phases use the same model. The workflow uses Anthropic's prompt caching for GENERATE calls.

## Cost Breakdown by Phase

Based on Haiku 4.5 pricing: $1/M input, $5/M output, $1.25/M cache write, $0.10/M cache read.

| Phase | Input Tokens | Output Tokens | Cache | Cost | Notes |
|-------|-------------|---------------|-------|------|-------|
| CLASSIFY | ~1,450 | ~117 | - | $0.002 | 1 call, simple routing |
| DECOMPOSE | ~4,580 | ~361 | - | $0.006 | 1 call, domain knowledge |
| GENERATE (×4) | ~300 | ~1,500 | 37K write + 111K read | $0.061 | Parallel SQL generation |
| SYNTHESIZE | ~6,400 | ~495 | - | $0.009 | 1 call, result synthesis |
| **Total** | ~12,700 | ~2,500 | 148K | **~$0.078** | Per analysis |

The GENERATE phase dominates cost due to the ~37K token system prompt (GENERATE.md + database schema).

## Optimizations

### Implemented: Cache Warming (PR: this commit)

**Problem:** Parallel GENERATE calls launched simultaneously, causing each to create a separate cache entry (race condition). With 4 parallel calls, this meant 4× cache creation cost.

**Solution:** Run the first GENERATE call synchronously to warm the cache, then run remaining calls in parallel.

**Savings:**
- Before: 4 × 37K × $1.25/M = $0.185 (4 cache creations)
- After: 1 × 37K × $1.25/M + 3 × 37K × $0.10/M = $0.058 (1 create + 3 reads)
- **~$0.127 saved per analysis (69% reduction in cache costs)**

### Evaluated: Subset Schema Filtering

**Idea:** Provide only relevant tables to GENERATE instead of full schema.

**Analysis:**
- Potential savings: ~50% reduction in schema tokens (~$0.03/analysis)
- Risks:
  - Cache fragmentation (different query types = different cache keys)
  - Accuracy loss from missing tables for cross-domain queries
  - Implementation complexity (need to classify before GENERATE)

**Decision:** Not implemented. The cache warming fix provides most of the benefit with no accuracy risk.

### Evaluated: Cheaper Models for Simple Phases

**Candidates:**
- CLASSIFY with local Ollama: ~$0.002 savings/query
- RESPOND with local Ollama: ~$0.003 savings/conversational query

**Decision:** Not implemented. Savings are marginal and the code already notes "Ollama models generate unreliable SQL queries."

## Cost Summary

| Scenario | Cost/Analysis | Notes |
|----------|--------------|-------|
| Before optimization | ~$0.20 | Multiple cache creations |
| After cache warming | ~$0.08 | Single cache creation |
| Full eval suite (28 tests) | ~$2.00 | ~385K tokens + caching |

The eval script now includes cost estimates in its output. Example:

```
=== Estimated Cost (Haiku 4.5) ===
  input:       $0.31 (312K tokens @ $1/M)
  output:      $0.37 (73K tokens @ $5/M)
  cache write: $1.15 (917K tokens @ $1.25/M)
  cache read:  $0.18 (1.8M tokens @ $0.10/M)
  ---
  total: $2.01
```

## Future Optimization Opportunities

1. **Reduce GENERATE.md examples** - Currently ~6K tokens of examples, some may be redundant
2. **Schema summarization** - Include only table index + relevant domain details
3. **Batch processing** - Use Anthropic's batch API for non-interactive workloads (50% output discount)

## References

- [Anthropic Pricing](https://www.anthropic.com/pricing)
- Prompt files: `agent/pkg/workflow/v3/prompts/`
- Workflow code: `agent/pkg/workflow/v3/pipeline.go`
