# Local stress-test harness

`run-stress-local.sh` brings up a containerized devnet (ledger + manager +
controller + funder, via `dev/dzctl start`), adds one custom **stress device**
container whose EOS startup config does NOT include the `doublezero-agent`
daemon, then launches the orchestrator and observer against it. The
orchestrator owns the agent lifecycle (starts it over SSH); the observer
samples the device.

Components:

| Piece                                | Image / source                                                 |
| ------------------------------------ | -------------------------------------------------------------- |
| Ledger / manager / controller        | e2e harness images (`dz-local/{ledger,manager,controller}`)    |
| Stress device                        | `tools/stress/docker/device/Dockerfile` (extends e2e device)   |
| Agent invocation wrapper             | `tools/stress/docker/device/agent-wrapper.sh` (in stress image)|
| Stress orchestrator                  | `tools/stress/device-orchestrator/cmd/device-orchestrator`     |
| Stress observer                      | `tools/stress/device-observer/cmd/device-observer`             |

## Quick start

```bash
# Full build + run (creates a 4-user sweep with 30s holds by default)
tools/stress/scripts/run-stress-local.sh --clean

# Skip docker build on subsequent runs
tools/stress/scripts/run-stress-local.sh --no-build

# Tweak the sweep
tools/stress/scripts/run-stress-local.sh --target-users 8 --hold 60
```

The script ends by printing the orchestrator/observer PIDs and the run
working directory (under `dev/.deploy/dz-local/stress/run/<UTC timestamp>/`).
Both processes keep running in the background. Stop them with the
`kill $(cat …)` snippet the script prints.

## What the stress device differs from the e2e device

It is the same cEOS base, but the startup config (rendered at run time
by the script) drops the `daemon doublezero-agent` and
`daemon doublezero-telemetry` blocks. cEOS pins admin's NSS shell to
`/usr/bin/RunCli` (the EOS Cli wrapper), so the image adds a separate
`stress` system user with `/bin/bash`; the script plants the
orchestrator's pubkey into its authorized_keys at runtime, and the
orchestrator's SSH session connects as `stress`.

## Agent metrics port: why 50100, not 9100

The agent's prometheus listener is parked on `:50100`, not the default
`:9100`. The cEOS device's `system control-plane` binds
`MAIN-CONTROL-PLANE-ACL` (no `-MGMT` suffix), and the
doublezero-controller's pushed device config fully redefines that ACL
on every apply (starting with `no ip access-list
MAIN-CONTROL-PLANE-ACL`). Any port permit we add via our startup-config
is wiped on the first agent apply. The controller's default ACL does
permit TCP `50000-50100`, so the wrapper at
`/usr/local/bin/doublezero-agent` sets `-metrics-addr :50100` and the
script points the observer's `--agent-metrics-url` at the same port.

## Caveats / known issues

- The orchestrator's hardcoded SSH command is
  `doublezero-agent -verbose [-controller HOST:PORT]`. It does not pass
  `-pubkey` or `-metrics-enable`. The stress image works around this
  with the wrapper at `/usr/local/bin/doublezero-agent`, which injects
  `-pubkey` from `/etc/doublezero/agent/pubkey` and turns on metrics on
  `:50100`.
- Use `--no-agent` to skip the SSH agent entirely; the orchestrator
  will only drive the onchain sweep and the observer will only see
  passive device state (no agent-log / metrics rows). Useful as a
  first smoke test.

## Teardown

```bash
dev/dzctl destroy -y
docker rm -f dz-local-device-dzstress 2>/dev/null
```
