# device-observer

> Alpha — eAPI sampler and Prometheus scrape are wired up. Log tailers
> (PR #3795) and abort decider (PR #3796) are no-op stubs in this revision.

`device-observer` samples an Arista cEOS device-under-test (DUT) during the
GRE Tunnel Capacity Study sweep. On every tick of `--sample-interval` it
issues five `show` commands over eAPI and writes one file per command into
the working directory, and it scrapes the doublezero-agent's Prometheus
metrics endpoint and appends rows to `observer.agent_metrics.json`.

It is designed to be driven by an external orchestrator: the orchestrator
sets the flags, owns the working directory, and signals the observer to
stop via `SIGINT` or `SIGTERM`.

## Build

The tool is built by the workspace `make go-build` target. To build only
this binary:

```sh
go build ./tools/stress/device-observer/cmd/device-observer
```

## Flags

| Flag                  | Default                 | Required | Purpose                                         |
| --------------------- | ----------------------- | -------- | ----------------------------------------------- |
| `--dut-host`          |                         | yes      | DUT hostname for eAPI                           |
| `--eapi-user`         | `admin`                 |          | eAPI username                                   |
| `--eapi-pass`         | `admin`                 |          | eAPI password (not persisted)                   |
| `--eapi-port`         | `80`                    |          | eAPI HTTP port                                  |
| `--agent-metrics-url` |                         | yes      | doublezero-agent Prometheus metrics URL         |
| `--sample-interval`   | `10s`                   |          | interval between eAPI samples                   |
| `--working-dir`       |                         | yes      | working directory for observer outputs          |
| `--abort-file`        | `<working-dir>/abort`   |          | path to write the abort sentinel file           |

## Working-directory contract

The observer writes the following files into `--working-dir`:

| File                                     | Owner     | Description                                     |
| ---------------------------------------- | --------- | ----------------------------------------------- |
| `observer-config.json`                   | observer  | selected flag values (excludes `eapi_pass` and `eapi_port`) + PID + start timestamp |
| `show-hardware-capacity-<ts>.json`       | observer  | one per tick                                    |
| `show-gre-tunnel-static-<ts>.json`       | observer  | one per tick                                    |
| `show-processes-top-once-<ts>.json`      | observer  | one per tick                                    |
| `show-logging-errors-<ts>.log`           | observer  | one per tick                                    |
| `show-logging-critical-<ts>.log`         | observer  | one per tick                                    |
| `observer.agent_metrics.json`            | observer  | NDJSON, one row per metric sample, appended     |

The orchestrator additionally owns these files in the same directory; the
observer reads them (in later PRs) but does not write them:

| File                          | Owner        | Read by                       |
| ----------------------------- | ------------ | ----------------------------- |
| `orchestrator-runlog.json`    | orchestrator | observer (PR #3795)           |
| `orchestrator.agent.log`      | orchestrator | observer (PR #3795)           |

The abort sentinel at `--abort-file` (default `<working-dir>/abort`) is
written by the observer (in PR #3796) and read by the orchestrator.

Filenames use the observer's local clock formatted as ISO 8601 UTC with
nanosecond precision, with `:` replaced by `-` for filesystem portability
(e.g. `show-hardware-capacity-2026-05-29T12-34-56.123456789Z.json`).

### `observer-config.json` schema

```json
{
  "started_at": "2026-05-29T12:34:56.789Z",
  "pid": 12345,
  "dut_host": "dz-local-device-dz1",
  "eapi_user": "admin",
  "agent_metrics_url": "http://dz-local-device-dz1:9100/metrics",
  "sample_interval": "10s",
  "abort_file": "/tmp/observer-out/abort",
  "working_dir": "/tmp/observer-out"
}
```

`eapi_pass` is deliberately omitted — the working directory may be archived
(e.g. to S3) and credentials must not land there.

## Metrics output

The Prometheus scraper fetches `--agent-metrics-url` on every tick, parses
the exposition-format response, and appends one row per metric sample to
`observer.agent_metrics.json` as newline-delimited JSON. The file name is
kept as `.json` for orchestrator compatibility, but the content is NDJSON
(one JSON object per line) so writes are append-safe across crashes.

Row schema:

```json
{
  "t_ns": 1748520896123456789,
  "metric_name": "doublezero_agent_apply_config_errors_total",
  "value": 0,
  "labels_json": "{}"
}
```

| Field          | Type    | Description                                                            |
| -------------- | ------- | ---------------------------------------------------------------------- |
| `t_ns`         | int64   | UTC unix-nano timestamp captured at the top of the tick (same for all rows in a tick) |
| `metric_name`  | string  | Prometheus family name, with `_sum`/`_count`/`_bucket` suffixes for summary and histogram series |
| `value`        | number  | metric value as float64 (counters and gauges; quantiles/buckets for summary/histogram) |
| `labels_json`  | string  | compact JSON object of label name → value (empty object `{}` when no labels) |

`labels_json` is stored as a JSON-encoded string rather than a nested object
so the file can be consumed with line-oriented tools (`jq -c`, ClickHouse
JSONEachRow) without schema-on-write decisions about variable label sets.

Counter family totals (sum across all label series) are also exposed via
`Scraper.Snapshot()` for in-process consumers; the abort decider in
PR #3796 will use this to detect mid-sample counter increments.

A per-tick HTTP failure, parse failure, or write failure is logged at WARN
and the loop continues — the abort decider owns repeated-failure policy.

## Local devnet smoke test

Against `dz-local-device-dz1` (see top-level `CLAUDE.md` for devnet setup):

```sh
mkdir -p /tmp/observer-out
./device-observer \
  --dut-host dz-local-device-dz1 \
  --eapi-user admin --eapi-pass admin \
  --agent-metrics-url http://dz-local-device-dz1:9100/metrics \
  --working-dir /tmp/observer-out \
  --sample-interval 10s
# After ~20 s, Ctrl-C.
ls /tmp/observer-out
jq . /tmp/observer-out/observer-config.json
jq . /tmp/observer-out/show-hardware-capacity-*.json
# observer.agent_metrics.json is NDJSON — use jq -c per line:
head /tmp/observer-out/observer.agent_metrics.json | jq -c .
```

## Known limitations

- The eAPI client re-marshals the goeapi-decoded JSON response, so very
  large integer counters (greater than 2^53) may lose precision and map
  key ordering is not preserved. A follow-up will replace the goeapi call
  path with a direct eAPI HTTP POST so the per-command JSON is captured
  byte-for-byte.
- `goeapi.RunCommands` does not accept a `context.Context`. The sampler
  works around this with a goroutine + `select` on `ctx.Done()` so the
  observer exits promptly on `SIGINT`/`SIGTERM`; however, an in-flight
  eAPI request may still complete in the background after exit.

## Failure handling

A failure on a single `show` command logs at WARN and continues to the
next command. The next tick retries from a clean slate. The abort decider
(PR #3796) owns the policy for declaring repeated failures fatal.

On `SIGINT` / `SIGTERM` the observer cancels its context and exits without
finishing a partially-started tick. Each file is written via a single
`os.WriteFile`, so partial-file reads are possible but the orchestrator
does not read sample files during a sweep.

## Disk usage

`show hardware capacity` can produce multi-megabyte JSON on heavily
configured devices. The observer writes one file per tick and never
appends, so the working directory grows steadily during a sweep. Pruning
old samples is the orchestrator's responsibility.

## Layout

```
tools/stress/device-observer/
├── cmd/device-observer/main.go
├── internal/
│   ├── abort/         # PR #3796 (stub here)
│   ├── collector/     # Collector interface + Noop
│   ├── eapi/          # thin goeapi wrapper
│   ├── loggingtail/   # PR #3795 (stubs here)
│   ├── promscrape/    # Prometheus scraper for the doublezero-agent
│   ├── runlog/        # PR #3795 (stub here)
│   └── sample/        # eAPI sampler
└── README.md
```
