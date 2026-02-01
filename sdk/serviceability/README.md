# Serviceability SDK

Read-only multi-language SDK for the DoubleZero serviceability Solana program.

## Overview

The serviceability program manages the network topology: locations, exchanges, devices, links, users, multicast groups, contributors, and access passes. All account data is Borsh-serialized with a 1-byte `AccountType` discriminator as the first byte.

## Program IDs

| Environment | Program ID |
|-------------|-----------|
| mainnet-beta | `ser2VaTMAcYTaauMrTSfSrxBaUDq7BLNs2xfUugTAGv` |
| testnet | `DZtnuQ839pSaDMFG5q1ad2V95G82S5EC4RrB3Ndw2Heb` |
| devnet | `GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah` |
| localnet | `7CTniUa88iJKUHTrCkB4TjAoG6TD7AMivhQeuqN2LPtX` |

## Account Types

| AccountType | Value | Description |
|-------------|-------|-------------|
| GlobalState | 1 | Global system state and allowlists |
| GlobalConfig | 2 | Network configuration (ASNs, tunnel blocks) |
| Location | 3 | Geographic locations |
| Exchange | 4 | Network exchanges/PoPs |
| Device | 5 | Network devices with interfaces |
| Link | 6 | Connections between devices |
| User | 7 | End users/clients |
| MulticastGroup | 8 | Multicast groups |
| ProgramConfig | 9 | Program version info |
| Contributor | 10 | Infrastructure contributors |
| AccessPass | 11 | User access control |

## Languages

- **Go** (`go/`) — `go test ./sdk/serviceability/go/...`
- **Python** (`python/`) — `cd sdk/serviceability/python && uv run pytest`
- **TypeScript** (`typescript/`) — `cd sdk/serviceability/typescript && bun test`

## Test fixtures

Binary fixtures in `testdata/fixtures/` are generated from Rust structs to verify cross-language deserialization compatibility.

Regenerate fixtures:

```bash
cd testdata/fixtures/generate-fixtures && cargo run
```
