# Revenue Distribution SDK

Multi-language SDK for reading DoubleZero revenue distribution program accounts on Solana.

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
