# Fix ValidatorsByRegion Eval Test

## Issue Summary

The `ValidatorsByRegion` eval test is failing because the agent's query returns incorrect validator counts per region. The test expects `tok=3, nyc=2, lon=1` but the agent returns `tok=6, nyc=6, lon=6`.

## Failure Details

From `eval-runs/20260115_221657/failures.log`:

```
Database validation passed: tok=3, nyc=2, lon=1 validators

Agent's response:
| Region | Validators | Total Stake |
|--------|-----------|-------------|
| tok    | 6         | 14,400 SOL  |
| nyc    | 6         | 9,600 SOL   |
| lon    | 6         | 4,800 SOL   |
```

The database validation confirms correct seeding, but the agent's SQL query returns 2x the expected counts.

## Root Cause Hypothesis

The query is likely double-counting validators due to one of:

1. **Duplicate join paths** - The query to get metro regions joins through multiple tables (users -> devices -> metros) and may be producing cartesian products
2. **History table duplication** - Querying from `_history` tables instead of `_current` views
3. **Missing DISTINCT** - The aggregation query doesn't dedupe vote_pubkeys before counting

## Investigation Steps

1. Read the test file to understand the seeded data structure:
   - `agent/evals/validators_by_region_test.go`

2. Check the generated SQL query in the test logs to see the actual query structure

3. Verify the join path in `dz_validators_on_dz` view:
   - `migrations/0004_solana_validator_views.sql`

4. Test the query directly against seeded data to identify where duplication occurs

## Related Files

- `agent/evals/validators_by_region_test.go` - Test definition and seeding
- `migrations/0004_solana_validator_views.sql` - Validator views
- `agent/pkg/pipeline/prompts/GENERATE.md` - SQL generation guidance

## Possible Fixes

1. **Fix the view** - If `dz_validators_on_dz` is producing duplicates, fix the join logic
2. **Update GENERATE.md** - Add guidance to use DISTINCT or proper grouping for validator counts
3. **Add example query** - Include a "validators by region" example in the prompts

## Also Check

The `OffDZValidatorsByRegion` test has a related issue - it correctly finds 3 validators but the eval expects the agent to explain "why only 3 exist" when asked for "top 10". This may need an eval expectation adjustment rather than agent fix.
