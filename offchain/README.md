# DoubleZero Rewarder

Construct off-chain reward distribution from on-chain DZ telemetry data.

## Usage

```bash
$ ./target/release/rewards_calculator
error: the following required arguments were not provided:
  --before <BEFORE>
  --after <AFTER>

Usage: rewards_calculator --before <BEFORE> --after <AFTER>

For more information, try '--help'.
```

```bash
$ ./target/release/rewards_calculator --help
Off-chain rewards calculation for DoubleZero network

Usage: rewards_calculator [OPTIONS] --before <BEFORE> --after <AFTER>

Options:
  -l, --log-level <LOG_LEVEL>  Override log level (trace, debug, info, warn, error)
  -r, --rpc-url <RPC_URL>      Override RPC URL
  -s, --skip-third-party       Skip third-party data fetching
      --cache-dir <DIR>        Directory to save cache files for inspection
      --load-cache <DIR>       Load data from cache directory instead of fetching
      --cache-processed        Include processed metrics and Shapley inputs in cache
  -h, --help                   Print help
  -V, --version                Print version

Time Range:
  -b, --before <BEFORE>  End timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T10:00:00Z), Unix timestamp (1705315200), or relative time (2 hours ago)
  -a, --after <AFTER>    Start timestamp for the rewards period (required) Accepts: ISO 8601 (2024-01-15T08:00:00Z), Unix timestamp (1705308000), or relative time (4 hours ago)
```
