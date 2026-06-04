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

The script ends by printing the orchestrator/observer/watcher PIDs and
the run working directory (under
`dev/.deploy/dz-local/stress/run/<UTC timestamp>/`). All three keep
running in the background. Stop them with the `kill $(cat …)` snippet
the script prints.

When the orchestrator exits (sweep finished or aborted), a background
**watcher** stops the observer and emits a post-run markdown summary
via [`tools/stress/device-reporter`](../device-reporter/) at
`<run-dir>/summary.md`. `tail -F summary.md` to read it as soon as it
lands, or invoke `device-reporter summary <run-dir>` ad hoc at any
point during the run for a partial view.

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

---

# Physical-device harness

`run-stress-physical.sh` reuses the same orchestrator/observer binaries
against a real Arista EOS device, with a host-local controller and the
devnet ledger.

Differences from the containerized harness:

| | Containerized | Physical |
| --- | --- | --- |
| Ledger | local solana-test-validator | devnet RPC (`DZ_RPC_URL`) |
| Serviceability | deployed fresh per `dzctl start` | pre-deployed at `DZ_PROGRAM_ID`, initialized on first run |
| Controller | `dz-local-controller` container | `go run controlplane/controller/cmd/controller start ...` on the host |
| Device | cEOS container + agent wrapper script | physical DUT over SSH, no wrapper |
| Agent invocation | wrapper injects `-pubkey` + sudo | orchestrator passes `-pubkey` directly and prefixes `bash sudo /sbin/ip netns exec ns-management` (the `bash` keyword escapes EOS Cli into the shell; `sudo` provides CAP_SYS_ADMIN for `ip netns exec`) |

The orchestrator gained four additive flags (`--agent-binary`,
`--agent-command-prefix`, `--agent-pubkey`, `--agent-metrics-addr`) to
support the physical case. All default to empty so the containerized
path is unchanged.

## Prerequisites on the physical DUT

The script does not configure EOS — it assumes a few things are already
in place on the device. Without these the script will either fail SSH /
SDK checks or the agent will run but fail to apply config:

1. **SSH access for the operator.** A user with a `/bin/bash` login
   shell and passwordless sudo. cEOS pins `admin` to RunCli, so don't
   use `admin`; create a separate operator user (this harness defaults
   to `nik`) keyed with `$DUT_SSH_KEY.pub`.

2. **`doublezero-agent` binary on disk.** Built statically for
   `linux/amd64` and SCP'd to `$AGENT_BINARY` (default
   `/mnt/flash/doublezero-agent`, which persists across reboots).
   Build with `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o
   doublezero-agent ./controlplane/agent/cmd/agent` on a host with the
   repo checked out.

3. **EOS native gNMI provider** running in the management VRF — the
   doublezero-agent always dials its local EOS API at
   `127.0.0.1:9543` (see `controlplane/agent/cmd/agent/main.go`, the
   `--device` default). On real Arista EOS, the `provider eos-native`
   knob inside the gNMI block exposes that listener (cEOS containers
   ship with it on by default; real hardware does not). Tested
   stanza — substitute the device's own management IP for
   `listen-addresses`:
   ```
   management api gnmi
      transport grpc default
         port 57400
         vrf management
         listen-addresses 10.0.0.X
      provider eos-native
   ```
   Verify the agent's target is reachable: `bash sudo ip netns exec
   ns-management ss -ltn | grep 9543` from the device should show a
   LISTEN entry. If the run aborts with `apply_config_errors` and the
   agent log shows `dial tcp 127.0.0.1:9543: connect: connection
   refused`, this stanza is missing or misconfigured.

4. **eAPI HTTP-commands enabled + a stress user.** The device-observer
   scrapes `show gre tunnel static`, `show processes top once`, etc.
   over HTTP basic auth on each sample. Add a dedicated stress user
   (so the harness doesn't share the `admin` password) and enable
   the HTTP transport:
   ```
   username stress secret 0 stress
   !
   management api http-commands
      no shutdown
   ```
   The harness defaults `EAPI_USER=stress`; export `EAPI_PASS` before
   running (the script bails out fast if it's unset, since an empty
   password silently produces 401s and leaves the run dir with zero
   `show-*.{json,log}` captures).

5. **Management netns.** The orchestrator wraps the agent command in
   `ip netns exec <netns>` so it can reach the controller via the
   management interface and dial the SDK in the management VRF. EOS
   auto-creates one netns per configured VRF; the netns name matches
   the VRF name (e.g. `vrf management` → `ns-management`, `vrf mgmt` →
   `ns-mgmt`). The harness defaults to `ns-management` to match the
   long-form VRF name used by chi-dn-dzd5; override the
   `AGENT_COMMAND_PREFIX` env var and the `DZ_STRESS_DEVICE_MGMT_VRF`
   env var consistently if your device uses a different name (cEOS
   uses `vrf mgmt`, so the containerized harness configures
   `mgmt_vrf=mgmt` onchain). Confirm what's available with
   `bash sudo ip netns list`.

## Quick start

```bash
# Required: the stress-test serviceability program ID lives in the
# private infra repo, not here. Export it before running.
export DZ_PROGRAM_ID='<stress-program-id-from-infra-repo>'

# Required: eAPI password for the observer. EAPI_USER defaults to
# `stress` (the username the README's prerequisite step adds to the
# device); password has no default — the script fails fast if unset.
export EAPI_PASS=stress

# Optional overrides (defaults shown in the script header):
# export DZ_RPC_URL='https://...'
# export DUT_HOST=10.0.0.15
# export DUT_SSH_USER=nik
# export DUT_SSH_KEY=$HOME/.ssh/nik@malbeclabs.com
# export SOLANA_KEYPAIR=$HOME/.config/doublezero/id.json

# 4-user smoke run
tools/stress/scripts/run-stress-physical.sh --target-users 4 --users-per-batch 2 --hold 0
```

The script:
1. SSH-pings the DUT and verifies `$AGENT_BINARY` exists on it.
2. Initializes the serviceability program (global-config + one location +
   one exchange + contributor `co01`) if not already initialized.
3. Creates the device + loopbacks onchain (idempotent).
4. Launches the controller on the host with
   `--max-user-tunnel-slots $TARGET_USERS` and waits for the gRPC port.
5. Sets up access passes in parallel against the devnet RPC.
6. Builds the orchestrator + observer + reporter binaries.
7. Launches the orchestrator + observer in the background, plus a
   background watcher that waits for the orchestrator to exit, then
   stops the observer and emits a markdown summary at
   `<run-dir>/summary.md` via
   [`tools/stress/device-reporter`](../device-reporter/).

The controller pid is recorded in `dev/.deploy/stress-physical/run/controller.pid`;
the orchestrator + observer + watcher pids land in the per-run subdirectory
(`run/<UTC timestamp>/`). Stop everything with the `kill $(cat …)`
snippet the script prints. `tail -F <run-dir>/summary.md` to watch the
post-run summary land.

## Overrides

All knobs are env vars (defaults in the script header):

| Var                       | Purpose                                                   |
| ------------------------- | --------------------------------------------------------- |
| `DZ_RPC_URL`              | Devnet RPC URL                                            |
| `DZ_PROGRAM_ID`           | Pre-deployed serviceability program ID                    |
| `DUT_HOST`                | Device IP/hostname                                        |
| `DUT_SSH_USER`            | SSH user on the device                                    |
| `DUT_SSH_KEY`             | SSH private key path                                      |
| `AGENT_BINARY`            | Path of `doublezero-agent` on the device                  |
| `AGENT_COMMAND_PREFIX`    | Prepended to the agent command (default: ns-management)   |
| `AGENT_METRICS_PORT`      | Metrics listener port on the device (default `50100`)     |
| `CONTROLLER_BIND_ADDR`    | Controller listen address (default `0.0.0.0`)             |
| `CONTROLLER_ADVERTISE_ADDR` | Address advertised to the device (default: auto-detect) |
| `CONTROLLER_LISTEN_PORT`  | Controller listen port (default `7000`)                   |
| `SOLANA_KEYPAIR`          | Operator keypair (signs init + access-passes)             |
| `EAPI_USER`               | eAPI HTTP basic-auth user (default `admin`)               |
| `EAPI_PASS`               | eAPI HTTP basic-auth password (no default — required)     |
| `DEVICE_CODE`             | Device code onchain (default `chi-dn-dzd5`)               |
| `DEVICE_DZ_PREFIX`        | Device's dz-prefix /29 (carved from the tunnel block)     |
