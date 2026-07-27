# Copilot instructions — doublezero

DoubleZero is a protocol for building and operating high-performance, permissionless networks — a
global dedicated-fiber network for distributed systems like blockchains. Service contributors
deploy devices and register them and their links onchain; users connect over GRE tunnels and receive
optimized routes via BGP, in unicast (IBRL) or multicast modes.

Two distinct chains are in play. The **DoubleZero Ledger** is the protocol's own Solana-based
cluster, run by its own validators; the serviceability, telemetry, geolocation, and
internet-latency programs in this repo are deployed there, reached via `ledger_rpc_url`.
**Solana L1** is a separate network reached via `solana_l1_rpc_url`: it carries the 2Z utility
token and hosts the shred-subscription program. The environment mapping between the two is not the
identity — DZ testnet's shred-subscription program lives on Solana devnet, not Solana testnet (see
`config/src/constants.rs`).

The repository is a hybrid Rust/Go monorepo: the Ledger programs and their CLI/SDK in
`smartcontract/` and `crates/`, the client daemon and controller in `client/` and `controlplane/`,
telemetry in `telemetry/`, end-to-end tests in `e2e/`, and read-only account decoders for
Go/Python/TypeScript in `sdk/`.

## Review posture

Report defects, not impressions. Every finding must name a concrete failure scenario — the input or
state that triggers it and the wrong behavior that results — and rate it by consequence. Label a nit
as a nit, and prefer a small number of well-evidenced findings over broad coverage. Check the
sibling implementation and the callers before reporting; several of the rules below exist because
the defect was only visible from outside the diff.

Most pull requests are fine. Reporting nothing is a valid and useful outcome — say the change looks
sound and stop there. Do not pad a review to look thorough, do not summarize back what the diff
already says, and do not raise a rule from these files merely because the diff touched the area it
covers. Every rule here describes a defect to look for, not a checklist to walk; a rule with no
matching problem in the diff has nothing to say.

A pre-existing defect is in scope when the diff widens its window, rewrites the lines that carry it,
or adds a second copy of the pattern. Say explicitly that it predates the change and what the change
did to make it matter, rate it accordingly — usually non-blocking — and do not attribute it to the
author.

When the root cause sits in a file the diff does not touch, anchor the comment on the nearest
changed line, say where the fix belongs, and say why this diff is what surfaces it.

State what you could not verify. Do not claim a test fails on the base branch, that a build is
clean, or that a downstream consumer breaks, unless the diff itself shows it — ask the author to
confirm instead.

## Universal rules

- Match the sibling. When a diff touches one member of a create/update/delete family or an A/B pair,
  the other members define the convention: guard placement, error variants, logging, layout
  comments, fail-fast strength. Two behaviors for one condition inside a single file is a finding.
- A comment, doc comment, RFC row, or CHANGELOG line that the diff makes false is worth reporting
  even when the code is correct. Rate it by what a reader would do wrong because of it: a rationale
  paragraph that survived a design pivot and now invites someone to "fix" working code is a real
  finding; a stale index in a layout comment is a nit.
- Claims made in the PR description and the CHANGELOG must be supported by the diff. Flag overstated
  ones: "asserts the specific error" when every branch returns the same code, "tightened
  expectations" when assertions were in fact loosened, "covers X" when no test reaches X.
- A new comparison, field, or branch that nothing reaches is a finding. Check the callers and say
  which fix applies — wire it up (a caller gates on `Equal` before ever consulting it) or delete it
  (nothing will ever reach it).
- Code that reads onchain state must use the endpoint for the chain that state lives on — the
  Ledger programs via `ledger_rpc_url`, the shred-subscription program and anything 2Z via
  `solana_l1_rpc_url` or its shred-specific override. Pointing a lookup at the wrong cluster does
  not error: the account simply does not exist there, so the caller silently takes its not-found
  path. Check the environment mapping rather than assuming DZ testnet means Solana testnet.
- Fail-open behavior and discarded errors (`|| true`, `_ = err`, `.map_err(|_| ...)`, a swallowed
  parse error) must be deliberate and must say so in a comment at the decision point.
- Use "onchain" as one word, never "on-chain".
