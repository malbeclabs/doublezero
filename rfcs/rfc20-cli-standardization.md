# RFC-20: CLI Standardization and Library Composition

## Summary

**Status: `Draft`**

This RFC defines the development standard for command-line tools in the DoubleZero project. Every module that exposes CLI functionality ships a Rust library crate that conforms to this standard. A single top-level `doublezero` binary composes those library crates at compile time and presents them to users and operators as one executable. The standard covers the module contract, argument and output conventions, the shared execution context, and the canonical backend client patterns for transacting against the DoubleZero ledger, Solana L1, the local daemon, and remote DoubleZero services.

All new CLI work in the project follows this standard.

## Motivation

DoubleZero is a multi-module system: a ledger of Solana programs, controllers and agents, a local user-side daemon, telemetry collectors, and SDKs in multiple languages. Each module exposes a CLI surface for operators, end users, or both. Without a written standard, every module reinvents argument naming, output formatting, error handling, and backend wiring, and the project ends up with multiple binaries that look and behave differently.

A single standard delivers three concrete benefits:

- **One binary** for users and operators to install, learn, and script against.
- **A predictable shape** for new CLIs, so adding a module's commands is mechanical rather than design work.
- **A single, citable document** that reviewers can point to when commands drift from the conventions.

The standard is restrictive on purpose. Most of its rules are not technically necessary; they exist so that every module looks the same.

## New Terminology

- **CLI module**: a Rust crate that contributes one or more top-level subcommands to the unified binary. Lives in this monorepo or in an external repository.
- **CLI binary**: the single `doublezero` executable that composes all CLI modules at compile time.
- **CLI core**: a shared crate providing the execution context, validators, formatters, output helpers, and preflight checks that every module reuses.
- **CliContext**: the runtime value the binary passes to every command. Carries the environment, resolved backend URLs and program IDs, signer, daemon socket, remote service endpoints, and an output format hint.
- **Backend client**: the typed client a command uses to perform its work. Four canonical kinds: DZ ledger, Solana L1, local daemon, and remote service API.
- **Verb**: a single user-facing action (`create`, `list`, `get`, `update`, `delete`, `connect`, `status`, ...).
- **Resource**: a noun a verb acts on (`device`, `link`, `user`, ...).

## Alternatives Considered

1. **Per-module binaries with a shared style guide.** Each module ships its own binary; the standard governs only conventions. Rejected: forces users to install and remember multiple binaries, and conventions invariably drift because nothing forces convergence.

2. **Mandatory grouped namespace** such as `doublezero ledger device create` and `doublezero daemon status` enforced for every module. Rejected: a flat top-level namespace is shorter for daily use and is the chosen default. Grouping remains available as a per-module choice (see Namespace and command organization); the rejection is of *forced* grouping for all modules.

3. **Polyglot binary linked via cgo or FFI.** Compile Go modules to c-archives and link from Rust. Rejected: cgo and FFI add toolchain complexity and slow build times for marginal benefit. Go-backed functionality is consumed by thin Rust clients over HTTP or gRPC.

4. **Positional arguments for primary identifiers** (kubectl-style). Rejected: named flags are explicit, order-independent, and stable under future schema extensions; scripts age better with named flags.

5. **A global `--output` flag** that every command honors uniformly. Rejected as too invasive on the module contract; per-command `--json` keeps coupling to the binary low.

6. **Runtime plugin discovery** (`libloading`-style dynamic modules). Rejected for this RFC: compile-time composition is sufficient for the project's needs and avoids ABI versioning between the binary and modules.

## Detailed Design

### Architecture

`doublezero` is the only CLI artifact the project ships. Its top-level subcommand enum aggregates one variant per CLI module; each module owns a library crate that exports its subcommand type and an `execute` method. The binary lists module crates in its `Cargo.toml`. Modules may live in this monorepo or in external repositories; both are integrated identically.

A shared CLI core crate provides the values and helpers every module reuses: the `CliContext` type, validators, formatters, output helpers, preflight checks, and the standard error type.

### Module contract

A CLI module crate **MUST**:

1. **Be a library-only crate** named `doublezero-<module>-cli`. The crate MUST NOT define a binary; the project ships exactly one binary, `doublezero`.

2. **Export at least one top-level subcommand type** that derives clap's `Subcommand`. Variants are the verbs the module owns. A module MAY export multiple subcommand enums if its verb groups are unrelated.

3. **Provide an async `execute` method** on each exported subcommand type that takes the parsed args, a reference to `CliContext`, and a writer for output, and returns a fallible result. The binary owns the async runtime and `await`s the verb. Verbs that perform no asynchronous work are still `async fn`; the runtime cost is negligible. Modules MUST NOT spin up their own runtime, call `block_on`, or otherwise hide async work behind a sync facade. No globals; all side effects flow through `ctx` and all output flows through the writer.

4. **Define per-verb argument and display types** colocated with the verb. Each verb is one file with the args type and any structured output type.

5. **Consume `CliContext` for all environment-derived inputs.** Modules MUST NOT read environment variables, configuration files, or `argv` directly. The binary populates `CliContext` once at startup; modules read from it.

6. **Use shared validators and formatters from the CLI core crate** wherever applicable. Modules MAY define module-specific validators, but the shared validators for pubkey, code, bandwidth, latency, and IPv4 MUST be used wherever those types appear.

7. **Send all output through the writer.** `println!`, `eprintln!`, and `print!` MUST NOT appear in execute paths. Errors propagate as `Result` values and are formatted by the binary.

A module **SHOULD** expose its backend client(s) behind a mockable trait, keep each verb in a single file, and provide unit tests that exercise `execute` against a mocked client.

### Backend client patterns

A verb interacts with exactly one of four backend patterns. A module MAY use multiple patterns across its verbs but SHOULD NOT mix patterns inside a single verb.

| Pattern | Transport | Configuration | Typical uses |
| ------- | --------- | ------------- | ------------ |
| DZ ledger | Solana RPC over HTTPS plus WebSocket | `--env` resolves URL, WS, and program IDs; `--url`, `--ws`, `--program-id`, `--geo-program-id` override per field | Transacting against DoubleZero programs |
| Solana L1 | Solana RPC over HTTPS | `--env` resolves the Solana L1 URL; `--solana-url` overrides | Generic Solana queries (account, balance, USDC, oracle reads) |
| Local daemon | HTTP over Unix domain socket | `--sock-file` overrides the default socket path | Controlling a local user-side daemon |
| Remote service API | HTTP or gRPC over TCP | `--env` resolves the default endpoint; module-owned `--<service>-url` overrides | Querying remote DoubleZero services (telemetry, controller, oracle) |

The DZ ledger and Solana L1 patterns are distinct backends and use separate override flags. The local daemon and remote service patterns are mechanically similar typed HTTP clients constructed from a URL or path in `CliContext`.

### CliContext

`CliContext` is the only value that crosses the boundary from the binary into module crates. The binary populates it once at startup from `--env` plus any explicit flag or environment-variable overrides, and modules treat it as read-only.

`CliContext` carries:

- The selected environment.
- The DZ ledger RPC URL and WebSocket URL.
- The Solana L1 RPC URL for the matching Solana network.
- Resolved DZ program IDs (serviceability, telemetry, geolocation, revenue distribution, ...).
- The signer keypair.
- The daemon Unix socket path.
- Default endpoints for known remote services.
- The output format hint (table, JSON, or compact JSON).

Modules MUST NOT mutate `CliContext` and MUST NOT re-resolve any value from `--env` themselves.

`CliContext` carries resolved configuration only: URLs, paths, identifiers, the signer, and the format hint. It does NOT expose typed backend clients (no `ctx.ledger_client()`, `ctx.daemon_client()`, etc.). Each module constructs its own typed clients from the values in `CliContext`. This keeps the CLI core crate a small utility library that depends only on clap, the logging facade, and the project's `config` crate; the Solana SDK, the daemon's HTTP stack, and any remote-service transports live with the modules that use them.

### Argument conventions

- **Named flags only.** Every input is a long flag in kebab-case. Positional arguments MUST NOT be used.

- **Short aliases on booleans only.** Boolean toggles MAY declare a single-letter short alias. Non-boolean flags MUST NOT use short aliases.

- **Identifiers accept both pubkey and code.** Any flag that references an onchain entity MUST accept either a Solana pubkey or the entity's human-readable code via the shared validator. Where a flag denotes a signer or payer-scoped entity (for example `--administrator`, `--user-payer`, `--contributor`), the verb MAY also accept the literal `"me"` and resolve it to the current payer's pubkey at execution time. `"me"` resolution is a verb-level responsibility, performed in the verb's `execute` path using the payer pubkey from `CliContext`; the shared validators only enforce grammar. Verbs that do not opt in will treat `"me"` as a literal code.

- **Repeatable inputs use one flag per value.** A list of permissions is `--add perm1 --add perm2`, not `--add perm1,perm2`. Exception: values that are naturally lists (such as CIDR prefix lists) MAY use a typed list parser.

- **Defaults are documented.** Magic runtime defaults such as `"me"` MUST be reflected in the flag's help text.

- **No environment-variable inputs at the verb level.** Verbs MUST NOT read process environment variables. Anything an operator might set in their environment is parsed at the binary's global-flag layer and surfaced through `CliContext`.

### Output conventions

- **Default output is a table.** Modules SHOULD use shared display helpers from the CLI core crate for common types (pubkey, bandwidth, latency, IPv4).

- **Every read, list, and get command MUST expose `--json`.** When set, the command writes pretty-printed JSON. The display type MUST be serializable, and pubkey fields MUST use the shared stable serializer.

- **Commands MAY additionally expose `--json-compact`** for single-line JSON suitable for piping. The flag name is fixed; alternative spellings MUST NOT be used.

- **Mutating commands** print the transaction signature and the post-confirmation status. They MAY accept `-w` or `--wait` to poll for activation.

- **YAML, CSV, and custom formats are out of scope.** Operators who need other formats pipe `--json` through `jq` or `yq`.

- **All user-facing output flows through the writer.** Modules MUST NOT write to standard output or standard error directly. Diagnostic logging is a separate channel (see Diagnostic logging below); the writer carries only the command's user-facing result.

### Diagnostic logging

Diagnostic output is separate from user-facing output and goes to standard error through the shared logging facade in the CLI core crate. Modules use the standard log macros (`debug!`, `info!`, `warn!`, `error!`, and `trace!` when finer granularity is justified) for anything that explains what a verb is doing internally: backend requests issued, retries, resolution of pubkey-or-code arguments, polling progress, and similar.

The binary configures the global log level from `--log-verbose`: warnings and errors only by default, `debug` when `--log-verbose` is set once, and `trace` when set twice (`--log-verbose --log-verbose`). The flag is spelled `--log-verbose` rather than `--verbose, -v` because `connect` and `disconnect` still own their own per-subcommand `--verbose` (`-v`) flags from earlier releases; a future RFC may deprecate those and reclaim the shorter spelling. Modules MUST NOT set or override the log level themselves and MUST NOT use `println!` or `eprintln!` for diagnostics. JSON output remains parseable regardless of `--log-verbose` because diagnostic logs go to stderr and the user-facing writer goes to stdout.

### Environments and configuration resolution

`--env` is the primary configuration knob. A single environment selector resolves every backend URL, program ID, and default service endpoint a verb might need. Operators set `--env` once; modules read fully resolved values from `CliContext`. Explicit per-field flag overrides exist for ad-hoc cases such as running against a private RPC pool, a local replica, or a custom program deployment.

The canonical environments are defined in the project's `config` crate. They are the same identifiers used by the `doublezero-solana` project and every other DoubleZero tool that selects between deployments; tools MUST share these names so that `--env testnet` reaches the same DZ ledger, the same Solana L1, and the same telemetry endpoints across every binary in the ecosystem.

| Env value | Short form | DZ ledger | Solana L1 | Notes |
| --------- | ---------- | --------- | --------- | ----- |
| `mainnet-beta` | `m` | DZ mainnet-beta ledger | Solana mainnet-beta | Production. |
| `testnet` | `t` | DZ testnet ledger | Solana testnet | Public test network. |
| `devnet` | `d` | DZ devnet ledger | Solana testnet | DZ developer network. Uses Solana testnet for L1 access; this asymmetry is intentional and matches the existing `config` crate mapping. |
| `local` | `l` | Local validator | Local validator | Local development. |

The override hierarchy is, in order of precedence (highest wins): explicit CLI flag, environment variable, value resolved from `--env`. Supported environment-variable overrides at the binary layer are exactly those honored by the `config` crate. Modules MUST NOT introduce verb-level overrides through environment variables.

### Global flags

The unified binary owns the following global flags, propagated to every subcommand. Modules MUST NOT redeclare or shadow them.

| Flag | Purpose |
| ---- | ------- |
| `--env` | Primary configuration knob. Selects the deployment and resolves every backend URL and program ID through the `config` crate. |
| `--url` | DZ ledger RPC URL override. Does NOT affect Solana L1. |
| `--ws` | DZ ledger WebSocket RPC URL override. |
| `--solana-url` | Solana L1 RPC URL override. Does NOT affect the DZ ledger. |
| `--keypair` | Path to the signer keypair file. |
| `--program-id` | Serviceability program ID override. |
| `--geo-program-id` | Geolocation program ID override. |
| `--sock-file` | Daemon Unix socket path override. |
| `--no-version-warning` | Suppress the version-check banner. |
| `--log-verbose` | Enable diagnostic logging. Repeating (`--log-verbose --log-verbose`) raises the level from `debug` to `trace`. No short alias yet because `connect`/`disconnect` still own `-v`/`--verbose` for legacy per-subcommand flags. |
| `--version`, `-V` | Print the binary version and exit. |

The DZ-ledger and Solana-L1 transports use separate override flags by design: confusing the two leads to verbs that quietly run against the wrong network. When `--env` is set, all transports resolve consistently; when an override is needed for one transport, the others continue to follow `--env`.

A module that needs a global input not in this table MUST extend the binary's flag set and the `config` crate's network configuration together via a follow-up RFC.

### Error handling and requirements

- All `execute` functions return a fallible result. The binary catches the top-level error and renders a single-line message followed by a chain of causes.

- Preflight checks compose from a shared bitflag type defined in the CLI core crate (keypair available, payer has balance, payer on allowlist, ...). Verbs invoke a single `check_requirements` call at the top of `execute`. Modules MAY define additional checks but SHOULD reuse the shared set first.

- **Authorization is enforced onchain.** The CLI is a thin client. The program rejects unauthorized signers; the CLI surfaces the error. Modules MUST NOT gate verbs by inspecting the caller's identity.

### Namespace and command organization

The top-level namespace is flat by default. Modules own their verbs and hoist them directly to the top level so users invoke `doublezero device create` rather than `doublezero <module> device create`. There is no mandatory backend grouping.

#### Flat vs grouped: a per-module choice

Each module decides whether to expose its commands flat at the top level or grouped under a single parent command. The default and preferred shape is flat. Grouping is permitted when it serves the module's users or avoids collisions with another module.

- **Flat exposure (default).** The module's subcommand enum is mounted flat, hoisting every variant to the top level. A ledger module contributes `device`, `link`, `user`, ... directly as top-level commands.

- **Grouped exposure.** The module's subcommand enum is mounted as a single named subcommand. A telemetry module that owns its own `device`, `agent`, and `link` views may mount itself as `telemetry`, so users invoke `doublezero telemetry device list`.

Grouped exposure is a property of how the binary mounts the module; the module crate itself exposes the same subcommand enum either way. Switching a module from flat to grouped is a one-attribute change in the binary and does not require any change in the module crate. The binary's top-level subcommand enum is the single source of truth for which mounting style each module uses.

#### Other rules

- **Resource-scoped verbs** are grouped as `<resource> <verb>` subcommands inside the module's enum (`device create`, `device list`, `device get`, `device update`, `device delete`). The standard verb set is `create`, `list`, `get`, `update`, `delete`; additional resource-specific verbs are permitted (`link accept`, `multicast publish`).

- **System-scoped verbs** are top-level inside the module's enum (`connect`, `disconnect`, `status`, `enable`, `disable`) when no single noun owns the action.

- **Verb naming.** Imperative and lowercase: `create`, not `new`. Past tense and gerunds MUST NOT be used.

- **Resource naming.** Singular nouns in kebab-case: `device`, `access-pass`, `multicast-group`. Plurals MUST NOT be used as command names.

- **Cross-module collisions.** Two flat-mounted modules MUST NOT register the same top-level command. When a collision is unavoidable, the module that registered the colliding name first keeps flat exposure; the later module switches to grouped exposure (or, if grouping is not appropriate, prefixes the colliding resource). Grouped exposure is the preferred resolution. Module owners SHOULD survey existing top-level commands before adding new ones.

- **Help ordering.** clap's default ordering applies. Modules MUST NOT partition help output by audience; admin and user verbs render together.

### Testing conventions

Three layers of tests are recognized; their weight reflects the cost-versus-value tradeoff:

- **Per-verb unit tests** in the module crate (MUST). Each verb is exercised by constructing the args directly, invoking `execute` with a mocked backend client and an in-memory writer, and asserting on the output. These are cheap and catch most regressions.

- **Per-module integration tests** that exercise the full clap argument parser from string inputs (SHOULD). The standard recommends but does not require these until the CLI core crate ships shared test helpers (a mockable `CliContext` builder, a verb-invocation macro, an assertion helper for table and JSON output). Once those helpers exist, per-module integration tests become MUST.

- **End-to-end tests** in the repository's top-level `e2e/` directory (MUST at the ecosystem level, not per module). These invoke the `doublezero` binary as a subprocess against real backends. E2E tests MUST NOT import module crates directly. New verbs SHOULD have at least one e2e scenario when they touch production paths.

The CLI core crate SHOULD ship the test helpers needed to make verb unit tests and per-module integration tests low-cost. Defining those helpers is part of the work of standing up the crate; the open question of their exact shape is tracked below.

### Crate layout

A CLI module crate is library-only and named `doublezero-<module>-cli`, where `<module>` is a single kebab-case noun describing the backend the module fronts. The crate exports its top-level subcommand enum from `lib.rs`, keeps each resource in its own subdirectory, and keeps each verb in its own file (args type, display type, and `execute` function colocated). Validators and display helpers live in module-local files only when the shared CLI core crate does not provide them.

### Composition in the binary

The binary lists each module crate as a dependency and adds one variant per module to its top-level subcommand enum. Each variant is mounted either flat (verbs hoist to the top level) or grouped (verbs sit under a single parent command). Dispatching from the parsed command to the module's `execute` method is one match arm per module.

Adding a new module is three changes in the binary: a `Cargo.toml` dependency, a variant in the top-level subcommand enum, and a dispatch arm. No other code changes.

## Impact

- **For contributors.** Every CLI verb has a single template. Code review can cite this RFC by section when a verb drifts.
- **For users and operators.** One binary, one help system, one flag style.
- **For SDK consumers.** None. SDKs in Go, TypeScript, and Python are out of scope.
- **For external operators.** A third-party operator who wants to extend the CLI publishes a crate that satisfies the module contract and builds a custom `doublezero` binary that lists their crate as a dependency. No fork of the main binary is required beyond the dependency edit.

### Migration of existing CLI surfaces

This RFC defines the target standard, not a migration mandate. Existing CLI surfaces (the serviceability resource verbs, daemon control, admin operations) are migrated to this standard opportunistically as they are touched for unrelated work; no deadline is set. Geolocation and the `doublezero-solana` surface are the natural first candidates because they are small, recently added, and not yet tangled with deep historical conventions. Until a given module is migrated, its current behavior is grandfathered and no new constraint is imposed retroactively. New modules and rewrites MUST conform.

## Security Considerations

- **Authorization is onchain.** The CLI does not enforce authorization. Verbs that require elevated privileges are visible to all users; the program rejects unauthorized transactions.

- **Keypair handling.** Keypair material is loaded once by the binary and held by `CliContext`. Modules MUST NOT re-read keypair files or pass keypair bytes outside the context.

- **External CLI modules.** Modules pulled from external repositories are linked at compile time and inherit the same trust as any other compile-time dependency. The binary's `Cargo.toml` is the trust boundary.

- **Daemon socket.** The Unix socket used for daemon communication is a local-only attack surface. Modules MUST honor the existing socket permission model.

- **JSON output as a contract.** When `--json` is set, downstream tooling is likely to parse the output programmatically. Modules MUST treat the JSON schema for each command as a public interface and version it accordingly.

## Backward Compatibility

This RFC is additive at the codebase level and non-breaking at the user-facing level.

- **Existing `doublezero` invocations continue to work.** The unified binary, its global flags, and its current subcommands are already the shape this RFC standardizes. No command name, flag name, output format, or exit-code semantics changes as a direct consequence of adopting this standard.
- **Existing user scripts are unaffected.** Operators and validators who script against `doublezero` today see no change in behavior until an individual module is migrated, and migrations preserve existing flag and output contracts (see *Migration of existing CLI surfaces* under Impact).
- **Pre-standard modules are grandfathered.** Modules that predate this RFC keep their current shape until they are touched for unrelated work. The standard imposes no retroactive version bump, deprecation, or forced cutover.
- **`--json` output is treated as a stable contract going forward.** Display types in migrated modules MUST NOT change in breaking ways without a JSON schema version bump; the mechanism for that bump is deferred to the follow-up RFC tracked in Open Questions.
- **External CLI module crates.** Third-party operators who maintain their own `doublezero` builds remain source-compatible with current module crates. Conforming to the module contract is required only for new modules and rewrites; existing external modules are unaffected until rebuilt against the standardized CLI core crate.
- **No on-disk, on-wire, or onchain state changes.** This RFC governs CLI ergonomics and Rust crate layout only. It does not change keypair files, daemon socket protocol, ledger account layouts, RPC schemas, or environment names; `--env testnet` resolves to the same network before and after adoption.

## Open Questions

1. **Runtime plugins.** Compile-time composition meets the project's stated needs. If external operators demonstrate the need for runtime-loadable modules, a follow-up RFC will define an ABI and a discovery mechanism.

2. **Shell completion.** clap supports generating completions across the full command tree. The standard SHOULD specify where completions install and how operators opt in; the details are deferred to a follow-up RFC.

3. **JSON schema versioning.** Once `--json` is a stable contract, breaking changes to display types require a versioning scheme. A follow-up RFC will define how the CLI surfaces a JSON schema version.

4. **Progress and interactivity.** The standard SHOULD define a single progress-reporting helper in the CLI core crate and the rules for when verbs may go interactive (TTY detection, `--no-progress`).

5. **Test helper API.** The exact shape of the shared test helpers in the CLI core crate (mockable `CliContext` builder, verb-invocation macro, table and JSON assertion helpers) is left to the crate's implementation. Once those helpers exist and are documented, the per-module integration test layer is upgraded from SHOULD to MUST in a follow-up RFC.

## Appendix A: Worked example

This appendix is illustrative, not normative. It shows what a small CLI module crate looks like end to end so that adopters have a concrete starting point. Identifiers, helper names, and crate dependencies are examples; the normative rules are in the Detailed Design section above.

```toml
# Cargo.toml
[package]
name    = "doublezero-geolocation-cli"
edition = "2021"

[lib]
# library only: no [[bin]]

[dependencies]
clap                  = { workspace = true, features = ["derive"] }
doublezero-cli-core   = { workspace = true }
# module-specific deps: SDK, transports, mock framework, ...
```

```rust
// src/lib.rs
pub use command::Command;
mod command;
mod probe;
```

```rust
// src/command.rs
use clap::Subcommand;
use doublezero_cli_core::{CliContext, Result};
use std::io::Write;

#[derive(Subcommand)]
pub enum Command {
    /// Probe a device for geolocation data.
    Probe(probe::ProbeArgs),
}

impl Command {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        match self {
            Self::Probe(args) => args.execute(ctx, out).await,
        }
    }
}
```

```rust
// src/probe.rs
use clap::Args;
use doublezero_cli_core::{
    CliContext, RequirementCheck, Result, validate_pubkey_or_code,
};
use std::io::Write;

#[derive(Args)]
pub struct ProbeArgs {
    /// Device identifier (pubkey or code).
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub device: String,
    /// Render output as pretty-printed JSON.
    #[arg(long)]
    pub json: bool,
}

impl ProbeArgs {
    pub async fn execute(self, ctx: &CliContext, out: &mut impl Write) -> Result<()> {
        ctx.check_requirements(RequirementCheck::KEYPAIR | RequirementCheck::BALANCE)?;
        let client = crate::client::GeoClient::from_context(ctx);
        let result = client.probe(&self.device).await?;
        if self.json {
            writeln!(out, "{}", serde_json::to_string_pretty(&result)?)?;
        } else {
            writeln!(out, "{}", doublezero_cli_core::render_table(&[result]))?;
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use doublezero_cli_core::testing::CliContextBuilder;

    #[tokio::test]
    async fn renders_table_by_default() {
        let ctx = CliContextBuilder::new().build();
        let mut out = Vec::new();
        let args = ProbeArgs { device: "dz1".into(), json: false };
        args.execute(&ctx, &mut out).await.unwrap();
        assert!(String::from_utf8(out).unwrap().contains("dz1"));
    }
}
```

The binary mounts this module with a single dependency, one variant in its top-level `Command` enum, and one dispatch arm. Switching the module from flat to grouped exposure is a one-attribute change on that variant in the binary; the module crate is unaffected.
