# DoubleZero Rewarder

Construct off-chain reward distribution from on-chain DZ telemetry data.

## RFC (in progress)

[DoubleZero#672](https://github.com/malbeclabs/doublezero/pull/672)

# TODO

- [ ] 3rd party latency data is undefined (therefore mocked)
- [ ] Substantially more testing required for correctness, verification and reproducibility
- [ ] All rewards are 0 because telemetry samples from devnet are 0s
- [ ] Last step of publishing artefacts to DZ Ledger is unimplemented

## Usage

- CLI

```bash
$ ./target/release/rewards_calculator -h
Off-chain rewards calculation for DoubleZero network

Usage: rewards_calculator [OPTIONS] --before <BEFORE> --after <AFTER>

Options:
  -l, --log-level <LOG_LEVEL>  Override log level (trace, debug, info, warn, error)
  -r, --rpc-url <RPC_URL>      Override RPC URL
  -s, --skip-third-party       Skip third-party data fetching
  -h, --help                   Print help
  -V, --version                Print version

Time Range:
  -b, --before <BEFORE>  End timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T10:00:00Z), Unix timestamp (1705315200), or relative time (2 hours ago)
  -a, --after <AFTER>    Start timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T08:00:00Z), Unix timestamp (1705308000), or relative time (4 hours ago)
```
