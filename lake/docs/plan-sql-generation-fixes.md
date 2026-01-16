# Plan: SQL Generation Issues

Two eval tests are failing due to SQL generation issues. These are flaky failures that occur intermittently.

## Issue 1: SolanaStakeShareDecrease

**Test:** `TestLake_Agent_Evals_Anthropic_SolanaStakeShareDecrease`

**Question:** "Why did the Solana network stake share decrease on DZ?"

**Expected:** Response should identify that `vote4` (5,000 SOL) and `vote5` (4,000 SOL) disconnected from DZ.

**Actual:** Agent focuses on data inconsistencies and query failures, failing to identify the disconnected validators with their stake amounts.

**Root Cause:** The generated SQL for finding disconnected validators is returning incorrect or incomplete results. The agent may be:
1. Using incorrect JOIN logic for the `solana_validators_on_dz_connections` view
2. Failing to properly compare current vs historical state
3. Getting confused by the history table snapshot pattern

**Relevant Prompt Section:** `GENERATE.md` - "Validators That Disconnected From DZ" (lines 279-298)

**Files to Investigate:**
- `agent/pkg/pipeline/prompts/GENERATE.md` - Check disconnection query examples
- `agent/evals/solana_stake_share_decrease_test.go` - Understand test data setup


## Issue 2: SolanaValidatorsConnectedStakeIncrease

**Test:** `TestLake_Agent_Evals_Anthropic_SolanaValidatorsConnectedStakeIncrease`

**Question:** "Which validators connected to DZ between 24 and 22 hours ago?"

**Expected:** Response should list `vote1` and `vote2` (connected during the 24-22 hour window).

**Actual:** Agent incorrectly lists `vote3` and `vote4` (which were already connected before this window).

**Root Cause:** The SQL time window filtering logic is inverted or using wrong comparison operators. The agent is likely:
1. Using `<` instead of `>` for time comparisons
2. Confusing "connected during window" with "connected before window"
3. Not properly using the `first_connected_ts` column from `solana_validators_on_dz_connections`

**Relevant Prompt Section:** `GENERATE.md` - "Validators That Connected During a Time Window" (lines 299-317)

**Files to Investigate:**
- `agent/pkg/pipeline/prompts/GENERATE.md` - Check time window query examples
- `agent/evals/solana_validators_connected_stake_increase_test.go` - Understand test data setup


## Recommended Approach

1. **Add more explicit examples** to GENERATE.md for these specific query patterns
2. **Clarify time comparison operators** in the prompt (e.g., `BETWEEN now() - INTERVAL 24 HOUR AND now() - INTERVAL 22 HOUR`)
3. **Add negative examples** showing common mistakes to avoid
4. **Consider adding integration tests** that verify the SQL patterns directly against ClickHouse


## Priority

Medium - These tests pass most of the time but fail intermittently, indicating the prompts could be clearer about these specific patterns.


## Status

- [x] Investigate SolanaStakeShareDecrease SQL patterns
- [x] Investigate SolanaValidatorsConnectedStakeIncrease SQL patterns
- [x] Update GENERATE.md with clearer examples
- [x] Update DECOMPOSE.md with time window guidance
- [x] Fix solana_validators_on_dz_connections view GROUP BY
- [x] Verify fixes with eval runs (4/4 passing)
