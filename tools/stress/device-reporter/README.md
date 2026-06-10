# device-reporter

Post-run analysis for the stress-test harnesses
(`tools/stress/scripts/run-stress-local.sh`,
`tools/stress/scripts/run-stress-physical.sh`).

Reads a run directory (`dev/.deploy/.../run/<UTC>/`), produces a
markdown summary.

## Build

```bash
go build -o /tmp/device-reporter ./tools/stress/device-reporter/cmd/device-reporter
```

## Usage

```
device-reporter summary <run-dir>     # markdown to stdout
```

## What the summary covers

- **Run knobs**: target users, batch size, hold, containerized vs physical.
- **Onchain phase timings**: provision / deprovision wall time, plus p50 and p95
  submit→activate gaps per phase.
- **Agent commit stats**: how many cycles ran, how many committed cleanly
  vs internally-aborted vs left unfinished, max config size seen, average
  commit duration.
- **Commit duration vs config size**: linear least-squares fit of commit
  duration against received-config bytes and lines, with R² so you can
  tell at a glance whether the relationship is roughly linear, loosely
  linear, or not well-fit by a line.
- **Agent CLI errors**: top normalized command patterns (tunnel IDs are
  collapsed so per-tunnel error spam buckets cleanly).
- **Abort detail**: trigger / reason / human-readable detail when an abort
  sentinel was written.

## Example

```bash
/tmp/device-reporter summary dev/.deploy/stress-physical/run/20260604T003155Z
```

```
# Stress run summary

**Run ID**: `run-cabb684ace4d58de`
**Target**: 512 users, batch=32, hold=—, dut=physical EOS
**Wall clock**: 6m22s
**Outcome**: **ABORTED** — trigger=`device_tunnel_gap` (...)

## Onchain

| Phase | Users | Wall time | p50 submit→activate | p95 |
|---|---|---|---|---|
| Provision | 512 | 3m12s | 11.82s | 12.09s |
| Deprovision | 512 | 3m10s | 11.85s | 12.10s |

## Agent

- Cycles: 13 (commits=13, internal aborts=0, unfinished=0)
- Avg commit duration: **2.48s**
- Max config received: **27,263 lines / 861,711 bytes**

### Commit duration vs config size

- vs **bytes**: slope = **1.83 µs/byte**, R² = **0.616** (loosely linear, n=13)
- vs **lines**: slope = **62.33 µs/line**, R² = **0.637** (loosely linear, n=13)
```
