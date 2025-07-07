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

## Example run log

```
doublezero-rewarder on  main via 🦀 v1.88.0 on ☁️  (us-east-1)
$ ./target/release/rewards_calculator --before "1 hours ago" --after "49 hours ago"
2025-07-07T18:00:19.300504Z  INFO Starting rewards_calculator v0.1.0
2025-07-07T18:00:19.300516Z  INFO Time range: 2025-07-05T17:00:19Z to 2025-07-07T17:00:19Z (172800 seconds)
2025-07-07T18:00:19.300522Z  INFO Initializing S3 publisher
2025-07-07T18:00:19.303122Z  INFO Created S3 publisher bucket=doublezero-rewards prefix=rewards
2025-07-07T18:00:19.303132Z  INFO S3 publisher initialized successfully
2025-07-07T18:00:19.303136Z  INFO Starting rewards calculation pipeline
2025-07-07T18:00:19.303139Z  INFO Phase 1: Fetching data from Solana and third-party sources
2025-07-07T18:00:19.303140Z  INFO Fetching data for time range: 1751734819300515 to 1751907619300515 microseconds
2025-07-07T18:00:19.303210Z  INFO Fetching all data for time range: 1751734819300515 to 1751907619300515 microseconds
2025-07-07T18:00:19.303216Z  INFO Using serviceability program: GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah
2025-07-07T18:00:19.303217Z  INFO Using telemetry program: C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG
2025-07-07T18:00:19.303318Z  INFO Fetching serviceability network data at timestamp 1751907619300515 from program GYhQDKuESrasNZGyhMJhGYFtbzNijYhcrN9poSqCQVah
2025-07-07T18:00:19.514332Z  INFO Found 13 serviceability accounts to process
2025-07-07T18:00:19.514369Z  INFO Processed 11 serviceability accounts: 1 locations, 1 exchanges, 4 devices, 1 links, 3 users, 1 multicast groups
2025-07-07T18:00:19.514374Z  INFO Fetching telemetry data for time range 1751734819300515 to 1751907619300515 from program C9xqH76NSm11pBS6maNnY163tWHT8Govww47uyEmSnoG
2025-07-07T18:00:19.597695Z  INFO Found 4 total telemetry accounts to process
2025-07-07T18:00:19.597717Z  INFO Filtered 1 telemetry accounts within time range (from 4 total, 0 errors)
2025-07-07T18:00:19.597719Z  INFO Telemetry statistics:
2025-07-07T18:00:19.597722Z  INFO   - Total latency samples: 1996
2025-07-07T18:00:19.597723Z  INFO   - Average samples per account: 1996
2025-07-07T18:00:19.597777Z  INFO Data fetch completed in 294.64ms
2025-07-07T18:00:19.597781Z  INFO Fetched data summary:
2025-07-07T18:00:19.597784Z  INFO   - Locations: 1
2025-07-07T18:00:19.597786Z  INFO   - Exchanges: 1
2025-07-07T18:00:19.597787Z  INFO   - Devices: 4
2025-07-07T18:00:19.597790Z  INFO   - Links: 1
2025-07-07T18:00:19.597792Z  INFO   - Users: 3
2025-07-07T18:00:19.597794Z  INFO   - Multicast Groups: 1
2025-07-07T18:00:19.597796Z  INFO   - Telemetry Samples: 1
2025-07-07T18:00:19.597798Z  INFO Sample telemetry data:
2025-07-07T18:00:19.597800Z  INFO   - Origin device: 5JcwAoBnsuwng78a21LQXh9LaQ6CLRapEkjXUxQw3chd
2025-07-07T18:00:19.597802Z  INFO   - Target device: B9xjyQCvhVJSAZW9xN2gnE9s7tyQ1ExxJ3jURnWTfFTX
2025-07-07T18:00:19.597804Z  INFO   - Link: 4CEJN5dMT2fBf5bfWv6muqqGbJiLRQYBjMYwqgbGs3Xb
2025-07-07T18:00:19.597806Z  INFO   - Start timestamp: 1751846402982238 μs
2025-07-07T18:00:19.597808Z  INFO   - Samples count: 1996
2025-07-07T18:00:19.597810Z  INFO   - Average latency: 0 μs
2025-07-07T18:00:19.597812Z  INFO Phase 2: Inserting data into DuckDB
2025-07-07T18:00:19.597814Z  INFO Creating in-memory DuckDB instance
2025-07-07T18:00:19.597816Z  INFO Creating in-memory DuckDB instance
2025-07-07T18:00:19.611232Z  INFO Inserting rewards data into DuckDB
2025-07-07T18:00:19.616798Z  INFO Successfully inserted all data into DuckDB
2025-07-07T18:00:19.616804Z  INFO Phase 3: Processing metrics
2025-07-07T18:00:19.616806Z  INFO Running SQL queries to aggregate metrics
2025-07-07T18:00:19.616809Z  INFO Processing metrics for Shapley calculation
2025-07-07T18:00:19.619692Z  INFO Processed links: 1 single-operator, 0 shared-operator
2025-07-07T18:00:19.619699Z  INFO Processed 1 private links
2025-07-07T18:00:19.621537Z  INFO Generated 1 public links
2025-07-07T18:00:19.623990Z  INFO Calculated 1 demand entries
2025-07-07T18:00:19.623997Z  INFO Metrics processing complete:
2025-07-07T18:00:19.623999Z  INFO   - Private links: 1
2025-07-07T18:00:19.624000Z  INFO   - Public links: 1
2025-07-07T18:00:19.624001Z  INFO   - Demand entries: 1
2025-07-07T18:00:19.624003Z  INFO Phase 4: Calculating rewards using Shapley values
2025-07-07T18:00:19.624005Z  INFO Calculating rewards distribution
2025-07-07T18:00:19.624007Z  INFO Calculating Shapley values for 1 private links, 1 public links, 1 demand entries
2025-07-07T18:00:19.624785Z  INFO Calculated rewards for 1 operators
2025-07-07T18:00:19.624789Z  INFO   - DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan: 0 (0%)
2025-07-07T18:00:19.625185Z  INFO Stored 1 rewards for epoch 1751907619300515
2025-07-07T18:00:19.625190Z  INFO Phase 5: Generating verification artifacts
2025-07-07T18:00:19.625191Z  INFO Generating verification packet and fingerprint
2025-07-07T18:00:19.625239Z  INFO Generated verification fingerprint: ded54a5591f0388d0f1b811164426c154be4da42b381cf063f2568b06b67c3a3
2025-07-07T18:00:19.625241Z  INFO Phase 6: Publishing results to S3
2025-07-07T18:00:19.625242Z  INFO Publishing artifacts to S3
2025-07-07T18:00:19.625245Z  INFO Publishing reward artifacts epoch=1751907619300515 prefix=year=2025/month=07/day=07/run-1751911219
2025-07-07T18:00:19.625420Z  INFO Publishing object to S3 with MD5 verification bucket=doublezero-rewards key=rewards/year=2025/month=07/day=07/run-1751911219/rewards.parquet size_bytes=1400 content_type=application/vnd.apache.parquet md5=d0TeKD5uWmZOsgLtxBw2zw==
2025-07-07T18:00:19.625826Z  INFO Publishing object to S3 with MD5 verification and encoding bucket=doublezero-rewards key=rewards/year=2025/month=07/day=07/run-1751911219/verification_packet.json.gz size_bytes=454 content_type=application/json content_encoding=gzip md5=4zQIreTuAkw4evfJh9epdg==
2025-07-07T18:00:19.625948Z  INFO Publishing object to S3 with MD5 verification bucket=doublezero-rewards key=rewards/year=2025/month=07/day=07/run-1751911219/verification_fingerprint.txt size_bytes=64 content_type=text/plain md5=nIDqljWubUyWiJVrIzk0wg==
2025-07-07T18:00:19.640490Z  INFO All artifacts uploaded successfully, writing _SUCCESS marker
2025-07-07T18:00:19.640495Z  INFO Publishing object to S3 bucket=doublezero-rewards key=rewards/year=2025/month=07/day=07/run-1751911219/_SUCCESS size_bytes=0 content_type=text/plain
2025-07-07T18:00:19.643142Z  INFO Successfully published object to S3 bucket=doublezero-rewards key=rewards/year=2025/month=07/day=07/run-1751911219/_SUCCESS
2025-07-07T18:00:19.643146Z  INFO Successfully published all artifacts with _SUCCESS marker bucket=doublezero-rewards prefix=year=2025/month=07/day=07/run-1751911219
2025-07-07T18:00:19.644265Z  INFO Rewards calculation completed successfully

doublezero-rewarder on  main via 🦀 v1.88.0 on ☁️  (us-east-1)
$ aws --endpoint-url=http://localhost:9000 s3 ls s3://doublezero-rewards/rewards/year=2025/month=07/day=07/run-1751911219/
2025-07-07 12:00:19          0 _SUCCESS
2025-07-07 12:00:19       1400 rewards.parquet
2025-07-07 12:00:19         64 verification_fingerprint.txt
2025-07-07 12:00:19        454 verification_packet.json.gz
```
