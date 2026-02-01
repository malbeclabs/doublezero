# Revenue Distribution SDK

Read-only multi-language SDK for the DoubleZero revenue distribution Solana program (`dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4`).

## Overview

The revenue distribution program manages epoch-based revenue collection from Solana validators and reward distribution to contributors. Each epoch:

1. Validator debts are calculated off-chain and committed as a Merkle root
2. Validators pay their debts (SOL transferred to distribution accounts)
3. Collected SOL is swapped to 2Z tokens via an on-chain swap program
4. Contributor rewards are calculated off-chain (Shapley values) and committed as a Merkle root
5. 2Z rewards are distributed to contributors based on their shares
6. A community burn rate determines what portion of 2Z is burned

## Data Sources

The SDK reads from two RPC endpoints:

**Solana RPC** — on-chain program accounts:

| Account | PDA Seeds | Description |
|---------|-----------|-------------|
| `ProgramConfig` | `["program_config"]` | Global configuration: admin keys, fee parameters, burn rate schedule |
| `Distribution` | `["distribution", epoch_le_bytes]` | Per-epoch state: debt/reward Merkle roots, payment tracking, burn amounts |
| `SolanaValidatorDeposit` | `["solana_validator_deposit", node_id]` | Per-validator deposit account; balance = lamports - rent exempt minimum |
| `ContributorRewards` | `["contributor_rewards", service_key]` | Per-contributor reward split configuration (up to 8 recipients) |
| `Journal` | `["journal"]` | Aggregate balance tracking across the program |

**DZ Ledger RPC** — ledger records (Borsh-serialized):

| Record | PDA Seeds | Description |
|--------|-----------|-------------|
| `ComputedSolanaValidatorDebts` | `["solana_validator_debt", epoch_le_bytes]` | Per-epoch validator debt calculations |
| `ShapleyOutputStorage` | `["reward_share", epoch_le_bytes]` | Per-epoch Shapley value reward shares |

## Languages

- **Go** (`go/`) — `go test ./sdk/revdist/go/...`
- **Python** (`python/`) — `cd sdk/revdist/python && uv run pytest`
- **TypeScript** (`typescript/`) — `cd sdk/revdist/typescript && bun test`

## Test fixtures

Binary fixtures in `testdata/fixtures/` are generated from Rust structs to verify cross-language deserialization compatibility.

Regenerate fixtures:

```bash
cd testdata/fixtures/generate-fixtures && cargo run
```
