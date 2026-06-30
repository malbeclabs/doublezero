# DoubleZero Offchain

## Code conventions

### Naming and prose

- Always write "onchain", never "on-chain".
- **No abbreviations for account or variable names — anywhere.** Spell out `validator_client_rewards`, not `vcr`. `validator_publisher_rewards`, not `vpr`. `shred_distribution`, not `sd`. This applies to source variable names, test bindings, code comments, program logs, PR titles, branch names, commit subjects, and any prose in this codebase. Acronyms that are universally known in the Solana/SPL ecosystem are fine — `PDA`, `ATA`, `CPI`, `SPL`, `SVM`, `IDL`. Project-specific shorthand is not — write out the full name even if it appears many times in a single function.

### Type annotations

- **Never** define types anywhere the Rust compiler can infer them. This is non-negotiable and applies everywhere — `let` bindings (source and tests), numeric literals, function-call turbofishes, test helpers, struct field initializers, function arguments. If `let x = ...;` compiles, do not write `let x: Foo = ...;`. If `5_000` compiles, do not write `5_000u16` or `5_000_u32`. If `.collect()` compiles, do not write `.collect::<Vec<_>>()`. **In particular, do not write `let mut x: u64 = 0;` — write `let mut x = 0;` and let the first usage drive inference.** Only add annotations when the compiler genuinely cannot infer. When in doubt, write without the annotation and add one only if the compiler asks.
- When the compiler does need a type hint, prefer **turbofish on the call site** (`.collect::<Vec<_>>()`, `<u32>::try_from(x)`) over annotating the `let` binding (`let xs: Vec<_> = ...`). Turbofish documents the type at the point it is needed; a let-binding annotation hides that intent and decays as the surrounding code changes.

### Comments and docstrings

- **`///` and `//!` docstrings are reserved for the crate's published API.** For structs, enums, type aliases, and constants that live inside a crate with no published rustdoc consumer, do not write `///` rustdoc on the item or on its fields. Field-level comments that just restate the field name are noise. The type and the field's name should carry the meaning. If a field's behavior or rationale is genuinely non-obvious (cross-variant naming, security-relevant invariants, contract between caller and helper), use a single `//` line comment above the field. Function and method docstrings on internal items are not covered by this rule. Those are fine.

    `///` on a `pub` function or method is allowed only when that item is reachable from the crate root through a `pub` module chain (a `pub` item inside a private submodule is not part of the published surface, so it follows the internal rule). `//!` module-level docs are allowed only on `pub` modules and must be one short line.

### Error handling

- **Prefer the `anyhow::Context` trait over the `anyhow!` macro.** Reach for `.context("...")` / `.with_context(|| ...)` rather than `.ok_or_else(|| anyhow!("..."))` on `Option<T>` or `.map_err(|e| anyhow!(e))` on `Result<T, E>`. For a `Result<T, String>` (or any `Display + Send + Sync + 'static` error), use `.map_err(anyhow::Error::msg)`. `anyhow!` and `bail!` are reserved for cases where you genuinely need to construct a new error from scratch with no Option/Result to attach context to — `ensure!`/`bail!` for invariant violations are fine.

### Abstraction discipline

- **Don't extract a helper function for code that only one caller uses.** Inline it. Helpers earn their keep by deduplicating logic across multiple call sites; a single-call-site helper just hides the work at the cost of an extra hop. The exception is when the helper is itself the unit under test (a parser, math routine, fixture builder being directly verified). Applies to source code, test code, and build/tooling scripts alike.
- **Do not add a new workspace crate without explicit ask.** New top-level crates change the workspace layout, bring CI surface area, and force a decision about naming, dependencies, and feature flags. Before proposing one, ask. If the work fits inside an existing crate (even if the crate's purpose stretches slightly), prefer extending the existing crate. The same rule applies to splitting a crate into multiple crates or merging existing crates.

### Tests

- **Every test function name starts with `test_`.** Write `#[test] fn test_<what_it_checks>()`, not `#[test] fn <what_it_checks>()`. Applies to `#[test]`, `#[tokio::test]`, unit tests, and integration tests alike.
