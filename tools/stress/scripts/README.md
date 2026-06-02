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

It is the same cEOS base, but the startup config (rendered at run time by the
script) drops the `daemon doublezero-agent` and `daemon doublezero-telemetry`
blocks. SSH access for `admin` is enabled via the orchestrator's
auto-generated ed25519 key, and the admin login shell in `/etc/passwd` is
flipped to `/bin/bash` after EOS boot so the SSH-exec'd
`doublezero-agent …` command runs through bash instead of the Cli parser.

## Caveats / known issues

- The orchestrator's hardcoded SSH command is
  `doublezero-agent -verbose [-controller HOST:PORT]`. It does not pass
  `-pubkey` or `-metrics-enable`. The stress image works around this with
  the wrapper at `/usr/local/bin/doublezero-agent`, which injects
  `-pubkey` from `/etc/doublezero/agent/pubkey` and turns on metrics on
  `:9100`.
- Use `--no-agent` to skip the SSH agent entirely; the orchestrator will
  only drive the onchain sweep and the observer will only see passive
  device state (no agent-log / metrics rows). Useful as a first smoke test.
- The observer's `agent_silence` and `apply_config_errors` triggers depend
  on the agent's metrics endpoint being reachable — they stay quiet under
  `--no-agent`.

## Teardown

```bash
dev/dzctl destroy -y
docker rm -f dz-local-device-dzstress 2>/dev/null
```
