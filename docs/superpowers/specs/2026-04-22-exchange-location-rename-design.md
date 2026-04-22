# Design: Rename exchange → metro, location → facility

## Context

The Serviceability program uses the concepts `exchange` (a network exchange point) and `location`
(a physical facility). These names no longer reflect domain language: the preferred terms are
`metro` and `facility`. This rename aligns the codebase vocabulary with current domain terminology
across all layers: onchain program, Rust SDKs, CLI commands, and external SDK consumers.

## Decisions

| Question | Decision |
|----------|----------|
| PDA seeds (`b"exchange"`, `b"location"`) | **Keep string values unchanged** — changing them would invalidate all existing onchain account addresses. Only the constant names and type names are renamed. A comment explains the discrepancy. |
| CLI subcommands | **Rename** — `doublezero exchange` → `doublezero metro`, `doublezero location` → `doublezero facility` |
| `AccountType::Display` strings | **Rename** — "exchange" → "metro", "location" → "facility" |
| Execution strategy | **One atomic PR** — single commit, compiler validates all references before committing |

## Scope

### 1. Onchain program (`smartcontract/programs/doublezero-serviceability/`)

**Files to rename:**
- `src/state/exchange.rs` → `src/state/metro.rs`
- `src/state/location.rs` → `src/state/facility.rs`
- `src/processors/exchange/` → `src/processors/metro/`
- `src/processors/location/` → `src/processors/facility/`

**Type renames:**
- `ExchangeAccount` → `MetroAccount`
- `LocationAccount` → `FacilityAccount`
- `AccountType::Exchange` → `AccountType::Metro`
- `AccountType::Location` → `AccountType::Facility`

**Instruction variant renames:**
- `CreateLocation` → `CreateFacility`, `UpdateLocation` → `UpdateFacility`, etc.
- `CreateExchange` → `CreateMetro`, `UpdateExchange` → `UpdateMetro`, etc.
- `SetDeviceExchange` → `SetDeviceMetro`

**Arg struct renames:**
- `ExchangeCreateArgs` → `MetroCreateArgs`, etc.
- `LocationCreateArgs` → `FacilityCreateArgs`, etc.

**Seeds (`src/seeds.rs`):**
- `SEED_EXCHANGE` → `SEED_METRO` (value stays `b"exchange"`)
- `SEED_LOCATION` → `SEED_FACILITY` (value stays `b"location"`)
- Add comment: `// Seed string kept as "exchange"/"location" to preserve existing onchain PDA addresses`

**AccountType Display:**
- `"location"` → `"facility"`
- `"exchange"` → `"metro"`

### 2. Smartcontract Rust SDK (`smartcontract/sdk/rs/`)

**Files to rename:**
- `src/commands/exchange/` → `src/commands/metro/`
- `src/commands/location/` → `src/commands/facility/`

**Type and function renames:** all `Exchange*` / `Location*` identifiers updated to `Metro*` / `Facility*`.

### 3. CLI (`smartcontract/cli/` and `client/doublezero/`)

**Files to rename:**
- `cli/src/exchange/` → `cli/src/metro/`
- `cli/src/location/` → `cli/src/facility/`

**Clap subcommand strings:**
- `"exchange"` → `"metro"` in all `#[command(name = "exchange")]` or string-based command definitions
- `"location"` → `"facility"` similarly

**Struct renames:** all `Exchange*` / `Location*` CLI structs updated.

### 4. Serviceability SDKs (`sdk/serviceability/`)

**Go SDK (`sdk/geolocation/go/`):**
- `GeolocationProgramConfig.ExchangePK` → `MetroPK` (serviceability foreign key)
- `GeolocationProgramConfig.LocationOffsetPort` — **verify**: if this field refers to the serviceability Location concept, rename to `FacilityOffsetPort`; if it's a standalone geographic concept, leave it

**Go SDK (`sdk/shreds/go/`):**
- `InstantSeatAllocationRequest.ExchangeKey` → `MetroKey`
- `MetroExchangeKey` field → `MetroKey` (or verify intent and rename accordingly)
- `DeriveMetroHistoryPDA` parameter `exchangeKey` → `metroKey`

**Python SDK (`sdk/serviceability/python/`):**
- Class names (`ExchangeAccount` → `MetroAccount`, `LocationAccount` → `FacilityAccount`), field names, seed constants

**TypeScript SDK (`sdk/serviceability/typescript/`):**
- Type names, field names, enum string keys (`LocationStatus` → `FacilityStatus`, `ExchangeStatus` → `MetroStatus`)
- Update `testdata/enum_strings.json`

**Fixture files:**
- `testdata/fixtures/exchange.bin` → `metro.bin`
- `testdata/fixtures/location.bin` → `facility.bin`
- Update all test references to these filenames

**Risk:** The word "location" appears in some fields as a generic concept (e.g., geographic coordinates or port offsets) rather than as a reference to the serviceability `LocationAccount`. During implementation, each `location`/`Location` occurrence in consumer code must be verified to confirm it refers to the serviceability type before renaming.

### 5. E2E tests (`e2e/`)

All Go test files that reference exchange/location types, CLI commands, or devnet setup helpers:
- `e2e/internal/devnet/` — smartcontract init, device setup
- `e2e/internal/qa/` — test helpers
- `e2e/internal/allocation/verifier.go`
- `e2e/internal/rpc/agent.go`

### 6. Consumers (Go)

- `controlplane/telemetry/internal/geoprobe/onchain_discovery.go`
- `telemetry/global-monitor/internal/dz/serviceability.go`
- `telemetry/flow-enricher/internal/flow-enricher/serviceability.go`
- `activator/` — any exchange/location references
- `api/` — any exchange/location references

## Execution Plan

1. **Rename directories** with `git mv` for proper git history tracking:
   - `processors/exchange` → `processors/metro`
   - `processors/location` → `processors/facility`
   - `cli/src/exchange` → `cli/src/metro`
   - `cli/src/location` → `cli/src/facility`
   - `sdk/rs/src/commands/exchange` → `sdk/rs/src/commands/metro`
   - `sdk/rs/src/commands/location` → `sdk/rs/src/commands/facility`
   - Fixture files

2. **Rename state files** with `git mv`:
   - `state/exchange.rs` → `state/metro.rs`
   - `state/location.rs` → `state/facility.rs`

3. **Mass identifier replace** using `sed` across all source files (excluding `.venv`, `node_modules`):
   - `ExchangeAccount` → `MetroAccount`
   - `LocationAccount` → `FacilityAccount`
   - `Exchange` → `Metro` / `exchange` → `metro` (with care for partial matches)
   - `Location` → `Facility` / `location` → `facility` (with care)
   - `SEED_EXCHANGE` → `SEED_METRO`
   - `SEED_LOCATION` → `SEED_FACILITY`

4. **Manual fixups** for seed string literals (`b"exchange"`, `b"location"`) — keep as-is, add comment.

5. **Build and iterate** — run `make rust-build` and `make go-build` to catch any missed references.

6. **Run linters** — `make rust-lint` and `make go-lint`.

7. **Run tests** — `make rust-test`, `make go-test`, `make sdk-test`.

8. **Regenerate fixtures** — `make generate-fixtures` to update `.bin` and `.json` fixtures.

## Verification

- `make build` passes cleanly (Rust + Go)
- `make rust-lint` passes with `-Dclippy::all -Dwarnings`
- `make go-lint` passes
- `make rust-test` passes
- `make sdk-test` passes (Go, Python, TypeScript SDKs)
- `doublezero metro list` and `doublezero facility list` CLI commands work correctly
- `doublezero metro get --code <code>` and `doublezero facility get --code <code>` work correctly
- JSON output from CLI shows `"metro"` and `"facility"` in account type fields
