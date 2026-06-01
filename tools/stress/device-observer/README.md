# device-observer

> Alpha — eAPI sampler, Prometheus scrape, and log tailers are wired up.
> The abort decider is still a no-op stub in this revision.

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
| `observer.eos_logging.json`              | observer  | NDJSON, one row per deduped `show logging last` entry, appended |
| `observer.agent_log.json`                | observer  | NDJSON, one row per agent-log line, appended    |

The orchestrator additionally owns these files in the same directory; the
observer reads them but does not write them:

| File                            | Owner        | Read by  |
| ------------------------------- | ------------ | -------- |
| `orchestrator-runlog.jsonl`     | orchestrator | observer |
| `orchestrator.agent.log`        | orchestrator | observer |

The abort sentinel at `--abort-file` (default `<working-dir>/abort`) is
written by the observer's abort decider and read by the orchestrator.

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
`Scraper.Snapshot()` for in-process consumers; the abort decider uses
this to detect mid-sample counter increments.

A per-tick HTTP failure, parse failure, or write failure is logged at WARN
and the loop continues — the abort decider owns repeated-failure policy.

## EOS syslog output

`observer.eos_logging.json` captures the device's syslog stream. Each tick
runs `show logging last <N> seconds` over eAPI where `N` is
`max(2 * sample_interval, 30 seconds)`, parses each line, and deduplicates
against the prior tick's set so overlap doesn't double-emit. The dedupe key
is `(timestamp, message)`.

Row schema:

```json
{
  "t_ns": 1748520896123456789,
  "time": "May 29 12:34:56",
  "severity": "3",
  "facility": "BGP",
  "message": "BGP-3-NOTIFICATION: ..."
}
```

Lines that don't match the default Arista syslog format (timestamp +
`FACILITY-SEV-MNEMONIC:` tag) still land in the file with empty `severity`
and `facility` and the full line under `message`, so unusual formats are
not silently dropped.

## Agent log output

`observer.agent_log.json` mirrors each new line of
`<working-dir>/orchestrator.agent.log` (written by the orchestrator's
SSH-backed agent runner). The tailer handles a missing file (returns no
rows until it appears), rotation (rename + recreate), and truncation.

Row schema:

```json
{
  "t_ns": 1748520896123456789,
  "line": "INFO: Committing config session due to diffs detected: ..."
}
```

`AgentTail.Snapshot()` also exposes:

- `LastLineAt` — wall-clock timestamp of the most recent observed line
  (zero before the first line). The abort decider uses this to flag
  agent silence beyond a threshold.
- `MatchCounts` — running counts of three abort-trigger substrings:
  `diff_timeout` ("could not get diff"), `lock_not_taken`
  ("not overriding lock since its age is less than"), and
  `commit_session` ("Committing config session due to diffs detected:").

## Runlog reader

`runlog.Reader` tails `<working-dir>/orchestrator-runlog.jsonl` (written
by the orchestrator) and pairs `submit` / `activate` and
`deprovision_submit` / `deprovision_activate` events by `user_index` to
produce per-user provision and deprovision durations. Durations are held
in bounded rings (1024 entries each).

`Reader.ProvisionDurations(window)` and `Reader.DeprovisionDurations(window)`
return the durations whose completion timestamp lies within `window` of
the current wall clock. The abort decider uses these to detect a
provisioning slowdown.

## Tailer behavior

The agent-log and runlog consumers share a poll-based tailer
(`internal/tailer`) that:

- treats a missing source file as a no-op (returns `nil, nil`) so the
  observer can start before the orchestrator creates the file,
- detects rotation via inode change and reads the new file from offset
  zero,
- detects truncation when the file size drops below the previously-read
  offset and reopens from offset zero,
- buffers a trailing fragment (data not yet terminated by `\n`) across
  polls so partial writes are not surfaced to the consumer.

Linux-only: inode comparison uses `syscall.Stat_t.Ino`. This matches the
device-observer's existing Linux-only assumptions (cEOS containers,
`/sys/class/net` for ifindex lookups).

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
owns the policy for declaring repeated failures fatal.

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
│   ├── abort/         # abort decider (stub)
│   ├── collector/     # Collector interface + Noop
│   ├── eapi/          # thin goeapi wrapper
│   ├── loggingtail/   # EOS-syslog poller + agent-log tailer
│   ├── promscrape/    # Prometheus scraper for the doublezero-agent
│   ├── runlog/        # orchestrator-runlog.jsonl reader + duration rings
│   ├── sample/        # eAPI sampler
│   └── tailer/        # shared poll-based file tailer
└── README.md
```
