# Telemetry SDK

Read-only multi-language SDK for the DoubleZero telemetry Solana program.

## Overview

The telemetry program stores latency measurement samples collected across the network. Each account contains a header with metadata and a vector of RTT samples in microseconds.

## Program IDs

| Environment | Program ID |
|-------------|-----------|
| mainnet-beta | `tE1exJ5VMyoC9ByZeSmgtNzJCFF74G9JAv338sJiqkC` |
| testnet | `3KogTMmVxc5eUHtjZnwm136H5P8tvPwVu4ufbGPvM7p1` |
| devnet | `C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG` |
| localnet | `C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG` |

## Account Types

| AccountType | Value | PDA Seeds | Description |
|-------------|-------|-----------|-------------|
| DeviceLatencySamples | 3 | `["telemetry", "dzlatency", origin, target, link, epoch]` | Device-to-device RTT samples |
| InternetLatencySamples | 4 | `["telemetry", "inetlatency", oracle, provider, origin, target, epoch]` | Internet latency samples |

## Languages

- **Go** (`go/`) — `go test ./sdk/telemetry/go/...`
- **Python** (`python/`) — `cd sdk/telemetry/python && uv run pytest`
- **TypeScript** (`typescript/`) — `cd sdk/telemetry/typescript && bun test`

## Test fixtures

Binary fixtures in `testdata/fixtures/` are generated from Rust structs to verify cross-language deserialization compatibility.

Regenerate fixtures:

```bash
cd testdata/fixtures/generate-fixtures && cargo run
```
