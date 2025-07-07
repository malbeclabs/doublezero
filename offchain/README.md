# DoubleZero Rewarder

Construct off-chain reward distribution from on-chain DZ telemetry data.

## RFC (in progress)

[DoubleZero#672](https://github.com/malbeclabs/doublezero/pull/672)

## Notes

- This follows the stateless architecture as described in the RFC as closely as possible.
- It does work end-to-end with the following caveats:
  - Substantially more testing required for correctness verification
  - All rewards are 0 because telemetry samples from devnet are 0s

## Usage

Example run for last 48 (minus 1 hr for delay) hours:

```bash
$ cp .envrc.sample .envrc # edit accordingly
$ just build
$ ./target/release/rewards_calculator  --before "1 hour ago" --after "49 hours ago"
```

## CLI

```bash
$ just
just -l
Available recipes:
    build     # Build (release)
    ci        # Run CI pipeline
    clean     # Clean
    clippy    # Run clippy
    cov       # Coverage
    default   # Default (list of commands)
    fmt       # Run fmt
    fmt-check # Check fmt
    test      # Run tests
```

```bash
$ ./target/release/rewards_calculator -h
Off-chain rewards calculation for DoubleZero network

Usage: rewards_calculator [OPTIONS] --before <BEFORE> --after <AFTER>

Options:
  -d, --dry-run                Enable dry run mode (no S3 uploads)
  -l, --log-level <LOG_LEVEL>  Override log level (trace, debug, info, warn, error)
  -r, --rpc-url <RPC_URL>      Override RPC URL
  -s, --skip-third-party       Skip third-party data fetching
      --cache-db               Cache fetched data to DuckDB file for development
      --load-db <PATH>         Load data from cached DuckDB file instead of fetching
  -h, --help                   Print help
  -V, --version                Print version

Time Range:
  -b, --before <BEFORE>  End timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T10:00:00Z), Unix timestamp (1705315200), or relative time (2 hours ago)
  -a, --after <AFTER>    Start timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T08:00:00Z), Unix timestamp (1705308000), or relative time (4 hours ago)
```
