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

- Sample log

```bash
$ ./target/release/rewards_calculator --before "1 hours ago" --after "49 hours ago"
2025-07-07T20:44:36.756656Z  INFO Starting rewards_calculator v0.1.0
2025-07-07T20:44:36.756677Z  INFO Time range: 2025-07-05T19:44:36Z to 2025-07-07T19:44:36Z (172799 seconds)
2025-07-07T20:44:36.756686Z  INFO Starting rewards calculation pipeline
2025-07-07T20:44:36.756688Z  INFO Phase 1: Fetching data from Solana and third-party sources
2025-07-07T20:44:36.756690Z  INFO Fetching data for time range: 1751744676756674 to 1751917476756673 microseconds
2025-07-07T20:44:36.756750Z  INFO Fetching all data for time range: 1751744676756674 to 1751917476756673 microseconds
2025-07-07T20:44:36.756752Z  INFO Using serviceability program: GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah
2025-07-07T20:44:36.756753Z  INFO Using telemetry program: C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG
2025-07-07T20:44:36.756867Z  INFO Fetching serviceability network data at timestamp 1751917476756673 from program GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah
2025-07-07T20:44:36.930953Z  INFO Found 13 serviceability accounts to process
2025-07-07T20:44:36.930990Z  INFO Processed 11 serviceability accounts: 1 locations, 1 exchanges, 4 devices, 1 links, 3 users, 1 multicast groups
2025-07-07T20:44:36.930993Z  INFO Fetching telemetry data for time range 1751744676756674 to 1751917476756673 from program C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG
2025-07-07T20:44:37.029480Z  INFO Found 4 total telemetry accounts to process
2025-07-07T20:44:37.029507Z  INFO Filtered 1 telemetry accounts within time range (from 4 total, 0 errors)
2025-07-07T20:44:37.029509Z  INFO Telemetry statistics:
2025-07-07T20:44:37.029511Z  INFO   - Total latency samples: 1996
2025-07-07T20:44:37.029513Z  INFO   - Average samples per account: 1996
2025-07-07T20:44:37.029584Z  INFO Data fetch completed in 272.89ms
2025-07-07T20:44:37.029586Z  INFO Fetched data summary:
2025-07-07T20:44:37.029587Z  INFO   - Locations: 1
2025-07-07T20:44:37.029588Z  INFO   - Exchanges: 1
2025-07-07T20:44:37.029590Z  INFO   - Devices: 4
2025-07-07T20:44:37.029591Z  INFO   - Links: 1
2025-07-07T20:44:37.029592Z  INFO   - Users: 3
2025-07-07T20:44:37.029593Z  INFO   - Multicast Groups: 1
2025-07-07T20:44:37.029594Z  INFO   - Telemetry Samples: 1
2025-07-07T20:44:37.029595Z  INFO Sample telemetry data:
2025-07-07T20:44:37.029596Z  INFO   - Origin device: 5JcwAoBnsuwng78a21LQXh9LaQ6CLRapEkjXUxQw3chd
2025-07-07T20:44:37.029598Z  INFO   - Target device: B9xjyQCvhVJSAZW9xN2gnE9s7tyQ1ExxJ3jURnWTfFTX
2025-07-07T20:44:37.029598Z  INFO   - Link: 4CEJN5dMT2fBf5bfWv6muqqGbJiLRQYBjMYwqgbGs3Xb
2025-07-07T20:44:37.029600Z  INFO   - Start timestamp: 1751846402982238 μs
2025-07-07T20:44:37.029601Z  INFO   - Samples count: 1996
2025-07-07T20:44:37.029604Z  INFO   - Average latency: 0 μs
2025-07-07T20:44:37.029605Z  INFO Phase 2: Processing metrics to construct shapley inputs
2025-07-07T20:44:37.049760Z  INFO Running SQL queries to aggregate metrics
2025-07-07T20:44:37.049773Z  INFO Processing metrics for Shapley calculation
2025-07-07T20:44:37.053090Z  INFO Processed links: 1 single-operator, 0 shared-operator
2025-07-07T20:44:37.053099Z  INFO Processed 1 private links
2025-07-07T20:44:37.055020Z  INFO Generated 1 public links
2025-07-07T20:44:37.057620Z  INFO Calculated 1 demand entries
2025-07-07T20:44:37.057629Z  INFO Metrics processing complete:
2025-07-07T20:44:37.057631Z  INFO   - Private links: 1
2025-07-07T20:44:37.057632Z  INFO   - Public links: 1
2025-07-07T20:44:37.057633Z  INFO   - Demand entries: 1
2025-07-07T20:44:37.057634Z  INFO Shapley inputs: ShapleyInputs {
    private_links: [
        Link {
            start: "chi1",
            end: "chi1",
            cost: 0.0110199999999999985467180608,
            bandwidth: 80000,
            operator1: "DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan",
            operator2: "0",
            uptime: 1,
            shared: 0,
            link_type: 0,
        },
    ],
    public_links: [
        Link {
            start: "chi1",
            end: "chi1",
            cost: 0.0489643134374693270172151926,
            bandwidth: 100,
            operator1: "0",
            operator2: "0",
            uptime: 1,
            shared: 0,
            link_type: 0,
        },
    ],
    demand_matrix: [
        Demand {
            start: "chi",
            end: "chi",
            traffic: 10,
            demand_type: 1,
        },
    ],
    reward_pool: 1000000000,
    demand_multiplier: 1.2,
}
2025-07-07T20:44:37.057657Z  INFO Phase 3: Calculating rewards using Shapley values
2025-07-07T20:44:37.057661Z  INFO Calculating rewards distribution
2025-07-07T20:44:37.057662Z  INFO Calculating Shapley values for 1 private links, 1 public links, 1 demand entries
2025-07-07T20:44:37.058518Z  INFO Calculated rewards for 1 operators
2025-07-07T20:44:37.058525Z  INFO   - DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan: 0 (0%)
2025-07-07T20:44:37.058948Z  INFO Stored 1 rewards for epoch 1751917476756673
2025-07-07T20:44:37.058954Z  INFO Phase 4: Generating verification artifacts
2025-07-07T20:44:37.058957Z  INFO Generating verification packet and fingerprint
2025-07-07T20:44:37.059006Z  INFO Generated verification fingerprint: b6cc25f0a8048d25ade4940fbd626ab8c5c0d712926224c70af55a4c0120b4e1
2025-07-07T20:44:37.059009Z  INFO verification_packet: VerificationPacket {
    packet_schema_version: "1.0.0",
    software_version: "0.1.0",
    shapley_version: "af862afadd264ca8d4967003ac922f3f1538ed4f",
    processing_timestamp_utc: "2025-07-07T20:44:37.059002846+00:00",
    epoch: 1751917476756673,
    slot: 1751917476756673,
    after_us: 1751744676756674,
    before_us: 1751917476756673,
    config_hash: "40df3a2f61e95650598e0ae19aa5110ae0e2bf940354edd4b72ad03ad27f7e3f",
    network_data_hash: "33bf73a041b6dd2afe6b0ba83329ead79d0d9d92a17a640d9767fc4d757087c4",
    telemetry_data_hash: "8cf45d68cd0268e022dd19198194af4f7176df38f6d919b18f8deee39e4f5850",
    third_party_data_hash: None,
    reward_pool: 1000000000,
    rewards: {
        "DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan": 0,
    },
}
2025-07-07T20:44:37.059022Z  INFO verification_fingerprint: VerificationFingerprint { hash: "b6cc25f0a8048d25ade4940fbd626ab8c5c0d712926224c70af55a4c0120b4e1" }

thread 'main' panicked at /home/rahul/malbec-labs/doublezero-rewarder/rewards_calculator/src/orchestrator.rs:85:9:
not yet implemented: Phase 5: Invoke program to publish artifacts to DZ Ledger
note: run with `RUST_BACKTRACE=1` environment variable to display a backtrace
```
