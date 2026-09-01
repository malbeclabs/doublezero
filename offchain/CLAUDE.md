# DoubleZero Offchain

## Code conventions

### Naming and prose

- Always write "onchain", never "on-chain".
- **No abbreviations for account or variable names — anywhere.** Spell out `validator_client_rewards`, not `vcr`. `validator_publisher_rewards`, not `vpr`. `shred_distribution`, not `sd`. This applies to source variable names, test bindings, code comments, program logs, PR titles, branch names, commit subjects, and any prose in this codebase. Acronyms that are universally known in the Solana/SPL ecosystem are fine — `tx`, `PDA`, `ATA`, `CPI`, `SPL`, `SVM`, `IDL`. Project-specific shorthand is not — write out the full name even if it appears many times in a single function.
- **Bindings that hold a `Pubkey` end in `_key`.** `manager_ata_key`, `program_config_key`, `validator_client_rewards_key`, and `holding_keys` for a collection. Covers locals, function parameters, and struct fields. An address and the account it names are usually both in scope, and a bare `destination` leaves the reader to work out which one they have. `node_id` keeps its spelling, since the name already denotes an address.
- **The `try_` prefix is reserved for functions returning `Result`.** A fallible-looking function returning `bool` or `Option` takes a plain name (`add_requested_feeds(...) -> bool`, not `try_add_requested_feeds`). Giving an existing function a `Result` return means renaming it to `try_`, and its callers with it. Where an existing `try_`-named function returns something else, treat it as a straggler rather than a pattern to copy.
- **Prose in this repo uses no contractions, no emdashes, and no sentence-chaining semicolons, and American spelling throughout.** Write "do not" rather than "don't", a comma or a period or parentheses rather than an emdash, two sentences rather than one joined by a semicolon, and "behavior" rather than "behaviour". This covers code comments, docstrings, READMEs, CHANGELOG entries, commit subjects, and PR and issue bodies. Semicolons inside a list are fine. The rule is for text you write or substantively change. Existing text, including the older parts of this file, is not worth a cleanup pass.

### Type annotations

- **Never** define types anywhere the Rust compiler can infer them. This is non-negotiable and applies everywhere — `let` bindings (source and tests), numeric literals, function-call turbofishes, test helpers, struct field initializers, function arguments. If `let x = ...;` compiles, do not write `let x: Foo = ...;`. If `5_000` compiles, do not write `5_000u16` or `5_000_u32`. If `.collect()` compiles, do not write `.collect::<Vec<_>>()`. **In particular, do not write `let mut x: u64 = 0;` — write `let mut x = 0;` and let the first usage drive inference.** Only add annotations when the compiler genuinely cannot infer. When in doubt, write without the annotation and add one only if the compiler asks.
- When the compiler does need a type hint, prefer **turbofish on the call site** (`.collect::<Vec<_>>()`, `<u32>::try_from(x)`) over annotating the `let` binding (`let xs: Vec<_> = ...`). Turbofish documents the type at the point it is needed; a let-binding annotation hides that intent and decays as the surrounding code changes.

### Numeric literals

- **Derive a constant in code, not in a comment.** When a constant is a sum of parts (a byte layout, a size reserve), write the sum with one term per line and a comment naming each term, then pin the total with `const _: () = assert!(NAME == 384);`. A literal carrying its arithmetic in a comment cannot be checked: the terms can fail to add up to it, and a term that changes leaves the literal stale with nothing to catch it. Group a long derivation into named block consts, each with its own assertion, and reference one from another where the same cost appears in both.
- **Annotate per literal, never above a run.** A comment above `3 + 1 + 96 + 32` leaves the reader mapping prose onto numbers by position, and gives no way to tell a wrong mapping from a right one. Break the expression across lines so each literal carries its own comment.
- **Do not pre-multiply, and do not repeat a literal that has a meaning.** `96` for three account keys hides both the count and which three, so write `3 * 32` where the items are interchangeable or one `32` per item where each has a name worth checking. A literal appearing in more than one expression is a constant that has not been named yet.
- Prefer a plain literal whose comment names the field over `size_of::<T>()`. Reach for `size_of::<T>()` where the width follows from a type that could plausibly change, or where the term is a composite no short comment makes obvious.
- All of the above apply in tests exactly as in source.

### Workspace dependencies

- **Put required features on the `[workspace.dependencies]` entry, not in a per-crate `{ workspace = true, features = [...] }` override.** Cargo unifies features across a build, so a per-crate feature is not isolated to that crate: every user of that dependency in the same build graph gets it anyway. The override buys no isolation and splits the dependency's feature set across manifests. A feature enabled in one member but missing from the workspace entry is the smell.

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

### Zero-copy account reads

- **Read an account through `SolanaConnection::try_fetch_zero_copy_data_with_commitment::<T>`.** It fetches, checks the discriminator, and checks the layout in one call. Reach for `ZeroCopyAccountOwnedData::from_account` only when the `Account` is already in hand, such as one element of a batched `try_fetch_multiple_accounts`, or when an absent account is an outcome the command reports itself rather than an error, since the helper folds absence into `Err`. `checked_from_bytes_with_discriminator` is for the case where only the bytes are in hand. Do not hand-roll the sequence of `get_account_with_commitment`, `.value`, and a discriminator check.
- **A `.with_context` message must be true for every error its call can return.** `try_fetch_zero_copy_data_with_commitment` returns `Err` for a transport failure, an absent account, and undecodable data alike, so a context reading "not initialized" states a diagnosis the code never established, and it is wrong whenever the RPC is unreachable. Name the read that failed and let the cause chain carry which failure it was.
