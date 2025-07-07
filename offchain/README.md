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

- Sample run

```bash
$ ./target/release/rewards_calculator --before "1 hours ago" --after "49 hours ago"
2025-07-07T20:01:18.647076Z  INFO Starting rewards_calculator v0.1.0
2025-07-07T20:01:18.647092Z  INFO Time range: 2025-07-05T19:01:18Z to 2025-07-07T19:01:18Z (172800 seconds)
2025-07-07T20:01:18.647098Z  INFO Starting rewards calculation pipeline
2025-07-07T20:01:18.647100Z  INFO Phase 1: Fetching data from Solana and third-party sources
2025-07-07T20:01:18.647101Z  INFO Fetching data for time range: 1751742078647090 to 1751914878647090 microseconds
2025-07-07T20:01:18.647157Z  INFO Fetching all data for time range: 1751742078647090 to 1751914878647090 microseconds
2025-07-07T20:01:18.647159Z  INFO Using serviceability program: GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah
2025-07-07T20:01:18.647159Z  INFO Using telemetry program: C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG
2025-07-07T20:01:18.647270Z  INFO Fetching serviceability network data at timestamp 1751914878647090 from program GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah
2025-07-07T20:01:18.832181Z  INFO Found 13 serviceability accounts to process
2025-07-07T20:01:18.832234Z  INFO Processed 11 serviceability accounts: 1 locations, 1 exchanges, 4 devices, 1 links, 3 users, 1 multicast groups
2025-07-07T20:01:18.832240Z  INFO Fetching telemetry data for time range 1751742078647090 to 1751914878647090 from program C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG
2025-07-07T20:01:18.920452Z  INFO Found 4 total telemetry accounts to process
2025-07-07T20:01:18.920490Z  INFO Filtered 1 telemetry accounts within time range (from 4 total, 0 errors)
2025-07-07T20:01:18.920495Z  INFO Telemetry statistics:
2025-07-07T20:01:18.920500Z  INFO   - Total latency samples: 1996
2025-07-07T20:01:18.920503Z  INFO   - Average samples per account: 1996
2025-07-07T20:01:18.920585Z  INFO Data fetch completed in 273.48ms
2025-07-07T20:01:18.920589Z  INFO Fetched data summary:
2025-07-07T20:01:18.920591Z  INFO   - Locations: 1
2025-07-07T20:01:18.920593Z  INFO   - Exchanges: 1
2025-07-07T20:01:18.920595Z  INFO   - Devices: 4
2025-07-07T20:01:18.920596Z  INFO   - Links: 1
2025-07-07T20:01:18.920598Z  INFO   - Users: 3
2025-07-07T20:01:18.920599Z  INFO   - Multicast Groups: 1
2025-07-07T20:01:18.920600Z  INFO   - Telemetry Samples: 1
2025-07-07T20:01:18.920602Z  INFO Sample telemetry data:
2025-07-07T20:01:18.920605Z  INFO   - Origin device: 5JcwAoBnsuwng78a21LQXh9LaQ6CLRapEkjXUxQw3chd
2025-07-07T20:01:18.920608Z  INFO   - Target device: B9xjyQCvhVJSAZW9xN2gnE9s7tyQ1ExxJ3jURnWTfFTX
2025-07-07T20:01:18.920610Z  INFO   - Link: 4CEJN5dMT2fBf5bfWv6muqqGbJiLRQYBjMYwqgbGs3Xb
2025-07-07T20:01:18.920611Z  INFO   - Start timestamp: 1751846402982238 μs
2025-07-07T20:01:18.920613Z  INFO   - Samples count: 1996
2025-07-07T20:01:18.920615Z  INFO   - Average latency: 0 μs
2025-07-07T20:01:18.920618Z  INFO Phase 2: Preparing metrics processing
2025-07-07T20:01:18.939713Z  INFO Phase 3: Processing metrics
2025-07-07T20:01:18.939726Z  INFO Running SQL queries to aggregate metrics
2025-07-07T20:01:18.939732Z  INFO Processing metrics for Shapley calculation
2025-07-07T20:01:18.943129Z  INFO Processed links: 1 single-operator, 0 shared-operator
2025-07-07T20:01:18.943135Z  INFO Processed 1 private links
2025-07-07T20:01:18.945010Z  INFO Generated 1 public links
2025-07-07T20:01:18.947435Z  INFO Calculated 1 demand entries
2025-07-07T20:01:18.947441Z  INFO Metrics processing complete:
2025-07-07T20:01:18.947443Z  INFO   - Private links: 1
2025-07-07T20:01:18.947445Z  INFO   - Public links: 1
2025-07-07T20:01:18.947446Z  INFO   - Demand entries: 1
2025-07-07T20:01:18.947447Z  INFO Phase 4: Calculating rewards using Shapley values
2025-07-07T20:01:18.947450Z  INFO Calculating rewards distribution
2025-07-07T20:01:18.947452Z  INFO Calculating Shapley values for 1 private links, 1 public links, 1 demand entries
2025-07-07T20:01:18.948357Z  INFO Calculated rewards for 1 operators
2025-07-07T20:01:18.948366Z  INFO   - DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan: 0 (0%)
2025-07-07T20:01:18.948946Z  INFO Stored 1 rewards for epoch 1751914878647090
2025-07-07T20:01:18.948952Z  INFO Phase 5: Generating verification artifacts
2025-07-07T20:01:18.948954Z  INFO Generating verification packet and fingerprint
2025-07-07T20:01:18.949008Z  INFO Generated verification fingerprint: 2fd1fbfa78bb63f399fcbe7df65d9ae14589a5d04f7470f5e2c1db68dda43136
2025-07-07T20:01:18.949011Z  INFO verification_packet: VerificationPacket {
    packet_schema_version: "1.0.0",
    software_version: "0.1.0",
    shapley_version: "af862afadd264ca8d4967003ac922f3f1538ed4f",
    processing_timestamp_utc: "2025-07-07T20:01:18.949004305+00:00",
    epoch: 1751914878647090,
    slot: 1751914878647090,
    after_us: 1751742078647090,
    before_us: 1751914878647090,
    config_hash: "40df3a2f61e95650598e0ae19aa5110ae0e2bf940354edd4b72ad03ad27f7e3f",
    network_data_hash: "33bf73a041b6dd2afe6b0ba83329ead79d0d9d92a17a640d9767fc4d757087c4",
    telemetry_data_hash: "8cf45d68cd0268e022dd19198194af4f7176df38f6d919b18f8deee39e4f5850",
    third_party_data_hash: None,
    reward_pool: 1000000000,
    rewards: {
        "DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan": 0,
    },
}
2025-07-07T20:01:18.949028Z  INFO verification_fingerprint: VerificationFingerprint { hash: "2fd1fbfa78bb63f399fcbe7df65d9ae14589a5d04f7470f5e2c1db68dda43136" }

thread 'main' panicked at /home/rahul/malbec-labs/doublezero-rewarder/rewards_calculator/src/orchestrator.rs:87:9:
not yet implemented: Phase 6: Invoke program to publish artifacts to DZ Ledger
note: run with `RUST_BACKTRACE=1` environment variable to display a backtrace
```
