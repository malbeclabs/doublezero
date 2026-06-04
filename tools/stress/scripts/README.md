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

3. **EOS SDK RPC agent (`eapilocal`)** running in the management VRF —
   the agent dials its local SDK at `127.0.0.1:9543`. cEOS provides
   this implicitly via `management api eos-sdk-rpc / transport grpc
   foo / localhost loopback`; physical EOS requires an explicit
   `daemon eapilocal` block plus a VRF qualifier on the transport:
   ```
   daemon eapilocal
      exec /usr/bin/EosSdkRpcAgent --daemon-name eapilocal
      no shutdown
   !
   management api eos-sdk-rpc
      transport grpc eapilocal
         localhost loopback vrf management
         service all
         no disabled
   !
   ```
   Verify with `show management api eos-sdk-rpc` — `Server: running`,
   `Listening on: ... port: 9543, VRF: management`.

4. **eAPI HTTP-commands enabled.** The device-observer scrapes `show
   gre tunnel static`, `show processes top once`, etc. via eAPI. Make
   sure `management api http-commands` is `no shutdown` and an admin
   user with a password the harness can use exists. Export
   `EAPI_USER` / `EAPI_PASS` before running.

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

# Required: eAPI credentials for the observer. EAPI_USER defaults to
# $DUT_SSH_USER; password must be supplied (no plaintext default).
export EAPI_USER=admin
read -s EAPI_PASS; export EAPI_PASS

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
6. Builds the orchestrator + observer binaries and launches them in the
   background, pointing at the physical DUT.

The controller pid is recorded in `dev/.deploy/stress-physical/run/controller.pid`;
the orchestrator + observer pids land in the per-run subdirectory
(`run/<UTC timestamp>/`). Stop everything with the `kill $(cat …)`
snippet the script prints.

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
