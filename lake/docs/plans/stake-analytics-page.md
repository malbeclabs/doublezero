# Stake Analytics Page

## Problem

Understanding DZ stake share changes is confusing. When stake share drops, it's unclear whether:
- A DZ validator left the network
- Delegators moved stake away from DZ validators
- New non-DZ validators joined (diluting share)
- Stake was redistributed among existing validators

The timeline shows validator join/leave events but doesn't capture stake redistribution, which is often the real cause of share changes.

## Proposed Solution

A dedicated **Stake Analytics** page that answers:
- What is the current DZ stake share?
- How has it changed over time?
- Why did it change? (with specific attribution)

## Features

### 1. Stake Share Overview
- Current DZ stake: X SOL (Y% of network)
- Large card/metric at top of page
- Comparison to 24h ago, 7d ago

### 2. Stake Share Timeline Chart
- Line chart showing DZ stake % over time
- Selectable time ranges (24h, 7d, 30d)
- Hover to see exact values at any point

### 3. Change Attribution Table
Shows what caused stake share changes in the selected period:

| Change Type | Impact | Details |
|------------|--------|---------|
| Stake redistributed away from DZ validators | -75,700 SOL | Cer1umMk... (-49k), Cw2b2ng2... (-26k) |
| Non-DZ validators joined | -0.5% share | 3 new validators with 50M SOL total |
| DZ validators joined | +0.2% share | 1 new validator with 20M SOL |
| Validators left network | 0 impact | Left validators were not on DZ |

### 4. Top DZ Validators Table
Sortable table showing:
- Validator (vote pubkey, truncated)
- Current stake (SOL)
- Stake share (%)
- 24h change (SOL, with +/- indicator)
- 7d change
- Connected device/metro

### 5. Stake Movement Events (Optional Timeline Addition)
Consider adding to main timeline:
- `stake_increased` - validator gained significant stake (>1% change or >10k SOL)
- `stake_decreased` - validator lost significant stake
- Threshold configurable to avoid noise

## Data Requirements

### New Queries Needed

1. **Stake share over time** - aggregate DZ validator stake at hourly intervals
```sql
SELECT
    toStartOfHour(snapshot_ts) as hour,
    sum(stake) as dz_stake,
    total_stake,
    dz_stake / total_stake as share_pct
FROM ...
GROUP BY hour
```

2. **Stake changes by validator** - compare current vs historical stake per validator
```sql
WITH current AS (...), historical AS (...)
SELECT vote_pubkey, current_stake, historical_stake, change
```

3. **Attribution analysis** - categorize changes by type
- Stake redistribution (existing validators, stake changed)
- New validators (first appearance in time range)
- Left validators (no longer in current)

### API Endpoints

1. `GET /api/stake/overview` - current stats and comparisons
2. `GET /api/stake/history?range=7d&interval=1h` - time series data
3. `GET /api/stake/changes?range=24h` - attribution breakdown
4. `GET /api/stake/validators` - paginated validator list with changes

## UI Components

### Files to Create
- `web/src/components/stake-page.tsx` - main page
- `web/src/components/stake-chart.tsx` - time series chart (recharts)
- `web/src/components/stake-attribution.tsx` - change breakdown
- `web/src/components/stake-validators-table.tsx` - validator list

### Navigation
- Add to sidebar: "Stake" with PieChart or TrendingUp icon
- Route: `/stake`

## Implementation Order

1. Backend: `GET /api/stake/overview` endpoint
2. Backend: `GET /api/stake/history` endpoint
3. Frontend: Basic page with overview metrics
4. Frontend: Stake share chart
5. Backend: `GET /api/stake/changes` endpoint
6. Frontend: Attribution table
7. Backend: `GET /api/stake/validators` endpoint
8. Frontend: Validators table
9. Optional: Add stake change events to timeline

## Open Questions

1. Should stake change events go in the main timeline or stay on dedicated page?
   - Pro: Unified view of all changes
   - Con: Could be noisy if many small changes
   - Suggestion: Only show significant changes (>1% or >10k SOL threshold)

2. How far back should history go?
   - Depends on data retention in history tables
   - Start with 30 days, extend if data available

3. Should we show non-DZ validator changes for context?
   - Useful to understand if DZ share dropped because network grew
   - Could add "Network overview" section
