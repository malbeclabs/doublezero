# DoubleZero SDKs

Read-only SDKs for deserializing DoubleZero onchain program accounts in Go, Python, and TypeScript.

- **serviceability** -- Serviceability program (contributors, access passes, devices, etc.)
- **telemetry** -- Telemetry program (metrics, reporting)
- **revdist** -- Revenue distribution program (epochs, claim tickets, etc.)
- **borsh-incremental** -- Shared Borsh deserialization library used by all three SDKs, implemented in each language

## Running Tests

```
make sdk-test          # Run all SDK tests (unit + fixture) across Go, Python, TypeScript
make sdk-compat-test   # Run compat tests against live RPC (requires network)
```

Per-SDK test commands:

| SDK | Go | Python | TypeScript |
|-----|----|----|------------|
| serviceability | `go test ./sdk/serviceability/go/...` | `cd sdk/serviceability/python && uv run pytest` | `cd sdk/serviceability/typescript && bun test` |
| telemetry | `go test ./sdk/telemetry/go/...` | `cd sdk/telemetry/python && uv run pytest` | `cd sdk/telemetry/typescript && bun test` |
| revdist | `go test ./sdk/revdist/go/...` | `cd sdk/revdist/python && uv run pytest` | `cd sdk/revdist/typescript && bun test` |

## Regenerating Fixtures

Each SDK has a Rust fixture generator at `testdata/fixtures/generate-fixtures/` that constructs account data using the actual onchain Rust types, Borsh-serializes them to `.bin` files, and writes expected field values to `.json` files. These fixtures are the source of truth -- they guarantee the binary data matches the real onchain serialization format.

**When to regenerate:** After modifying onchain Rust structs (adding/removing fields, changing enum variants, etc.), you must regenerate fixtures so the SDK tests reflect the updated serialization format.

**How to regenerate:**

```bash
# Regenerate fixtures for a specific SDK
cd sdk/serviceability/testdata/fixtures/generate-fixtures && cargo run
cd sdk/telemetry/testdata/fixtures/generate-fixtures && cargo run
cd sdk/revdist/testdata/fixtures/generate-fixtures && cargo run
```

After regenerating, update the deserialization logic in Go, Python, and TypeScript to handle any new or changed fields, then run `make sdk-test` to verify consistency across all three languages.

## Testing Strategy

### Cross-language fixture tests

Go, Python, and TypeScript each deserialize the same `.bin` files and verify every field value against the same `.json` expectations. If all three languages pass on the same fixture, they agree on deserialization.

### Compat tests

Hit live RPC endpoints to deserialize real onchain accounts, spot-checking key fields. Gated behind environment variables (`SERVICEABILITY_COMPAT_TEST=1`, `REVDIST_COMPAT_TEST=1`) since they require network access.

### Borsh-incremental unit tests

Comprehensive tests for the shared deserialization library in all three languages, covering primitive types, variable-length types, optional fields, and error cases.

### Enum string fixtures

A shared `enum_strings.json` file is verified by all three languages to ensure status/type enum string representations are consistent. Python's bidirectional check catches new variants added in any language.

### PDA derivation tests

Verify that program-derived addresses match known values across all three languages.

## Adding a New Field or Enum Variant

1. Update the Rust fixture generator and regenerate fixtures (`cargo run` from the generator directory).
2. Update deserialization logic in Go, Python, and TypeScript.
3. For new enum variants: update `enum_strings.json`, then update the enum definitions in all three languages.
4. Run `make sdk-test` to verify consistency.

## Directory Structure

```
sdk/
├── borsh-incremental/     # Shared deserialization library (Go, Python, TypeScript)
├── serviceability/        # Serviceability program SDK
│   ├── go/
│   ├── python/
│   ├── typescript/
│   └── testdata/fixtures/ # Rust-generated binary + JSON fixtures
├── telemetry/             # Telemetry program SDK
└── revdist/               # Revenue distribution program SDK
```

Each SDK follows the same layout with `go/`, `python/`, `typescript/` subdirectories and a shared `testdata/fixtures/` directory containing the Rust-generated test data.
