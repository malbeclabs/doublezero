# revdist — Revenue Distribution Go SDK

Read-only Go SDK for the DoubleZero revenue distribution Solana program (`dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4`).

## Overview

The revenue distribution program manages epoch-based revenue collection from network participants and reward distribution to contributors. Each epoch:

1. Validator debts are calculated off-chain and committed as a Merkle root
2. Validators pay their debts (SOL transferred to distribution accounts)
3. Collected SOL is swapped to 2Z tokens via an on-chain swap program
4. Contributor rewards are calculated off-chain (Shapley values) and committed as a Merkle root
5. 2Z rewards are distributed to contributors based on their shares
6. A community burn rate determines what portion of 2Z is burned

## Account Types

| Account | PDA Seeds | Description |
|---------|-----------|-------------|
| `ProgramConfig` | `["program_config"]` | Global configuration: admin keys, fee parameters, burn rate schedule |
| `Distribution` | `["distribution", epoch_le_bytes]` | Per-epoch state: debt/reward Merkle roots, payment tracking, burn amounts |
| `SolanaValidatorDeposit` | `["solana_validator_deposit", node_id]` | Per-validator deposit account; balance = lamports - rent exempt minimum |
| `ContributorRewards` | `["contributor_rewards", service_key]` | Per-contributor reward split configuration (up to 8 recipients) |
| `Journal` | `["journal"]` | Aggregate balance tracking across the program |

## Usage

```go
import (
    "github.com/gagliardetto/solana-go"
    solanarpc "github.com/gagliardetto/solana-go/rpc"
    "github.com/malbeclabs/doublezero/smartcontract/sdk/go/revdist"
)

programID := solana.MustPublicKeyFromBase58("dzrevZC94tBLwuHw1dyynZxaXTWyp7yocsinyEVPtt4")
rpcClient := solanarpc.New("https://api.mainnet-beta.solana.com")

client := revdist.New(rpcClient, programID)

// Fetch program config
config, err := client.FetchConfig(ctx)

// Fetch a specific epoch's distribution
dist, err := client.FetchDistribution(ctx, 42)

// Fetch a validator's deposit
deposit, err := client.FetchValidatorDeposit(ctx, nodeID)

// Get effective deposit balance (lamports - rent exempt)
balance, err := client.ValidatorDepositBalance(ctx, nodeID)

// Fetch all validator deposits
deposits, err := client.FetchAllValidatorDeposits(ctx)

// Fetch contributor rewards config
rewards, err := client.FetchContributorRewards(ctx, serviceKey)
```

### Off-Chain Records (DZ Ledger)

For accessing off-chain validator debt calculations and Shapley reward shares, provide a `LedgerRecordClient`:

```go
client := revdist.NewWithLedger(rpcClient, programID, ledgerClient)

debts, err := client.FetchValidatorDebts(ctx, epoch)
shares, err := client.FetchRewardShares(ctx, epoch)
```

### Oracle

```go
oracle := revdist.NewOracleClient("https://sol-2z-oracle-api-v1.mainnet-beta.doublezero.xyz")
rate, err := oracle.FetchSwapRate(ctx)
```

## Multi-Tenancy

While current revenue sources are Solana validators, the program is designed to accommodate future revenue sources. The naming uses generic terms (e.g., "distribution" rather than "validator distribution") where applicable.
