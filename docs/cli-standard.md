# DoubleZero CLI Standard (RFC-20)

This document is a contributor-facing summary of the CLI standard defined by
[RFC-20: CLI Standardization and Library Composition](../rfcs/rfc20-cli-standardization.md).
The RFC is the normative source; this page is the day-to-day reference for
writing a new verb or migrating an existing one.

## The shape

DoubleZero ships a single `doublezero` binary. The binary is thin: it parses
global flags, builds a `CliContext`, then dispatches to verbs that live in
**module crates**. Each module crate is a library named
`doublezero-<module>-cli` and conforms to a fixed contract.

```
doublezero (binary)                                client/doublezero/
  └─ CliContext + dispatch
doublezero-cli-core (shared library)               crates/doublezero-cli-core/
  ├─ CliContext, CliContextBuilder, OutputFormat
  ├─ RequirementCheck (preflight bitflags)
  ├─ shared validators (pubkey, code, bandwidth, latency, ...)
  ├─ display formatters
  ├─ init_logging (tracing facade)
  └─ testing helpers
doublezero-serviceability-cli (module)             smartcontract/cli/
doublezero-<future-module>-cli                     ...
```

The core crate stays small on purpose: it depends on `clap`, the logging
facade, `doublezero-config`, and `doublezero-program-common`. The Solana
SDK, daemon HTTP stack, and remote-service transports live with the module
crates that use them.

## The module contract

A CLI module crate **MUST**:

1. Be a library-only crate named `doublezero-<module>-cli`. No `[[bin]]`.
2. Export at least one top-level subcommand type that derives clap's
   `Subcommand`. Verbs are variants.
3. Provide an `async fn execute` on each subcommand type. The runtime lives
   in the binary; modules MUST NOT call `block_on` or hide async work behind
   a sync facade.
4. Define per-verb args and display types **colocated** with the verb.
5. Consume `CliContext` for environment-derived inputs. Modules MUST NOT
   read environment variables, configuration files, or `argv` directly.
6. Use the shared validators (`validate_pubkey`, `validate_pubkey_or_code`,
   `validate_code`, `validate_parse_bandwidth`, ...) from
   `doublezero_cli_core::validators` wherever those types appear.
7. Send all output through the writer. `println!`, `eprintln!`, and
   `print!` MUST NOT appear in execute paths. Diagnostic logging goes
   through `tracing` (stderr).

A module **SHOULD** keep each verb in a single file, expose its backend
client(s) behind a mockable trait, and provide per-verb unit tests against a
mocked client.

## Argument conventions

- Named flags only. No positional arguments.
- Long names in kebab-case.
- Short aliases on booleans only.
- Identifiers that reference an onchain entity use `validate_pubkey_or_code`
  and accept either a pubkey or the entity's code. Where a flag denotes a
  signer or payer-scoped entity (for example `--administrator`,
  `--user-payer`, `--contributor`), the verb MAY also accept the literal
  `"me"` and resolve it to the current payer's pubkey at execution time.
  `"me"` resolution is a verb-level responsibility, not a validator
  behavior; verbs that do not opt in will treat `"me"` as a literal code.
- Repeatable inputs use one flag per value (`--add a --add b`), not comma
  lists.
- No env-var reads at the verb level. Anything an operator might set in
  their environment is parsed at the binary's global-flag layer and
  surfaced through `CliContext`.

## Output conventions

- Default output is a table.
- Every `get`, `list`, and read command MUST expose `--json`. The display
  type MUST be `Serialize`. Pubkey fields use the shared stable
  serializer.
- Commands MAY additionally expose `--json-compact` for single-line JSON.
  The flag name is fixed.
- Mutating commands print the transaction signature and post-confirmation
  status.
- All user-facing output flows through the writer passed to `execute`.

## Global flags

The binary owns these globals; modules MUST NOT redeclare them:

| Flag | Purpose |
| ---- | ------- |
| `--env` | Primary config knob; selects deployment and resolves URLs, program IDs, and default service endpoints. |
| `--url` | DZ ledger RPC URL override (does NOT affect Solana L1). |
| `--ws` | DZ ledger WebSocket URL override. |
| `--solana-url` | Solana L1 RPC URL override (does NOT affect DZ ledger). |
| `--keypair` | Path to signer keypair file. |
| `--program-id` | Serviceability program ID override. |
| `--geo-program-id` | Geolocation program ID override. |
| `--sock-file` | Daemon Unix socket path override. |
| `--no-version-warning` | Suppress version-check banner. |
| `--log-verbose` | Diagnostic logging. Repeat for higher levels: once raises to `debug`, twice raises to `trace`. No short alias because `connect`/`disconnect` still own `-v`/`--verbose` for their own flags. |
| `--version`, `-V` | Print version and exit. |

`--env` resolves through `doublezero-config`. Recognized values are
`mainnet-beta`/`m`, `testnet`/`t`, `devnet`/`d`, `local`/`l`.

## Diagnostic logging

Diagnostic output goes to **stderr** via `tracing`. Modules use the
standard log macros (`debug!`, `info!`, `warn!`, `error!`, `trace!`) for
anything that explains what a verb is doing internally: backend requests,
retries, pubkey-or-code resolution, polling progress.

```rust
tracing::debug!(env = %ctx.env, code = %self.code, "location get");
```

Modules MUST NOT call `init_subscriber` themselves; the binary calls
`doublezero_cli_core::init_logging(verbosity)` once at startup. The
`RUST_LOG` env var overrides verbosity for per-module filtering.

JSON output on stdout stays parseable at every verbosity level because logs
go to stderr.

## Reference verb: `location get`

`smartcontract/cli/src/location/get.rs` is the worked example. It demonstrates
the conforming pattern end to end:

```rust
use clap::Args;
use doublezero_cli_core::CliContext;
use doublezero_sdk::commands::location::get::GetLocationCommand;
use serde::Serialize;
use std::io::Write;
use tabled::Tabled;

use crate::{doublezerocommand::CliCommand, validators::validate_pubkey_or_code};

#[derive(Args, Debug)]
pub struct GetLocationCliCommand {
    /// Location Pubkey or code to get details for
    #[arg(long, value_parser = validate_pubkey_or_code)]
    pub code: String,
    /// Output as JSON
    #[arg(long)]
    pub json: bool,
}

#[derive(Tabled, Serialize)]
struct LocationDisplay { /* ... */ }

impl GetLocationCliCommand {
    pub async fn execute<C: CliCommand, W: Write>(
        self,
        ctx: &CliContext,
        client: &C,
        out: &mut W,
    ) -> eyre::Result<()> {
        tracing::debug!(env = %ctx.env, code = %self.code, "location get");

        let (pubkey, location) = client.get_location(GetLocationCommand {
            pubkey_or_code: self.code,
        })?;

        let display = LocationDisplay { /* ... */ };
        if self.json {
            writeln!(out, "{}", serde_json::to_string_pretty(&display)?)?;
        } else {
            // render table via Tabled
        }
        Ok(())
    }
}
```

Unit test (excerpt):

```rust
use doublezero_cli_core::testing::cli_context_default_for_tests;

let ctx = cli_context_default_for_tests();
let mut output = Vec::new();
let res = block_on(
    GetLocationCliCommand { code: "test".into(), json: true }
        .execute(&ctx, &client, &mut output),
);
assert!(res.is_ok());
```

The test uses `MockCliCommand` (auto-generated by `#[automock]` on the
`CliCommand` trait) as the backend, and the shared
`cli_context_default_for_tests()` helper from
`doublezero_cli_core::testing` to build a `CliContext` with sensible
defaults.

## Preflight checks

Verbs MAY call `RequirementCheck` to gate on common preconditions:

```rust
use doublezero_cli_core::RequirementCheck;

let checks = RequirementCheck::KEYPAIR | RequirementCheck::BALANCE;
```

The bitflags align with the legacy `CHECK_ID_JSON | CHECK_BALANCE |
CHECK_FOUNDATION_ALLOWLIST` `u8` constants in
`smartcontract/cli/src/requirements.rs`:

| Flag | Bit |
| ---- | --- |
| `RequirementCheck::KEYPAIR` | `0b001` |
| `RequirementCheck::BALANCE` | `0b010` |
| `RequirementCheck::FOUNDATION_ALLOWLIST` | `0b100` |

The actual `check_requirements` function lives with the module that owns
the typed backend client (today, `smartcontract/cli/src/requirements.rs`).
The bitflag type is shared so future modules consume the same canonical
set.

## Authorization

Authorization is **onchain**. The CLI is a thin client. The program
rejects unauthorized signers; the CLI surfaces the error. Modules MUST NOT
gate verbs by inspecting the caller's identity.

## Migration is opportunistic

RFC-20 explicitly grandfathers existing CLI surfaces. Existing verbs keep
their current shape until they are touched for unrelated work. New verbs
MUST conform from day one. When you touch a legacy verb, prefer to
migrate it to the conforming pattern; if migration would balloon the
change, leave it for a follow-up and note it in the PR description.

## Open follow-ups

Tracked in RFC-20 §Open Questions and in this work's plan:

- Serviceability `Command` enum lives in the binary today; future PR moves
  it into `smartcontract/cli` with `#[command(flatten)]` mounting.
- Geolocation module crate (defer per current scope).
- Daemon-control verbs (Connect, Status, Enable, Disable, Latency, Routes)
  become their own module crate.
- JSON schema versioning once `--json` is a stable contract.
- Shell-completion install location.
