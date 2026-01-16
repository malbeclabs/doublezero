# Fix DZVsPublicInternet Eval Flakiness

## Problem

The `DZVsPublicInternet` eval test fails intermittently because the agent doesn't always include jitter/IPDV metrics in its response.

**Failure message:**
> The response fails to meet Expectation #2 (Jitter/IPDV comparison). While the response provides excellent side-by-side latency comparisons with numbers, mentions multiple metro-to-metro paths, and shows DZ outperforming the public internet, it does not include jitter or IPDV (Inter-Packet Delay Variation) metrics comparing the two networks.

## Root Cause

The test expects the response to include jitter/IPDV comparison, but:
1. The decomposition may not always ask for jitter metrics
2. The agent may have the data but not include it in synthesis
3. The expectation may be too strict if jitter data isn't always meaningful

## Investigation Needed

1. Check the test file to understand:
   - What data is seeded (does it include jitter metrics?)
   - What the expectations require exactly
   - Location: `agent/evals/dz_vs_public_internet_test.go`

2. Check recent failure logs to see:
   - What questions were decomposed
   - What data was returned
   - What the synthesized response contained

3. Determine if the fix should be:
   - **DECOMPOSE.md**: Add guidance to always ask for jitter when comparing networks
   - **SYNTHESIZE.md**: Add guidance to always report jitter when available
   - **Test expectation**: Relax if jitter data isn't always present/meaningful

## Potential Fixes

### Option A: Update DECOMPOSE prompt
Add guidance that network comparison questions should include jitter/IPDV metrics:
```
**For DZ vs public internet comparisons**: Always include jitter/IPDV metrics alongside latency and packet loss. Users need the full picture of network quality.
```

### Option B: Update SYNTHESIZE prompt
Add guidance to always report jitter when comparing network performance:
```
**Network comparisons**: When comparing DZ to public internet, always include: latency (avg, P95), packet loss, AND jitter/IPDV if available.
```

### Option C: Review test expectation
If jitter data isn't consistently meaningful or available, consider whether the expectation is too strict.

## Files to Review

- `agent/evals/dz_vs_public_internet_test.go` - test setup and expectations
- `agent/pkg/pipeline/prompts/DECOMPOSE.md` - decomposition guidance
- `agent/pkg/pipeline/prompts/SYNTHESIZE.md` - synthesis guidance
- `eval-runs/*/failures.log` - recent failure details
