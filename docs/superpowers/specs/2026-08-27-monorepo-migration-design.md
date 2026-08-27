# Absorbing doublezero-offchain and doublezero-solana into this repo

Target repo: malbeclabs/doublezero (this repo). Branch each step off `origin/main`.
Source repos: malbeclabs/doublezero-offchain, malbeclabs/doublezero-solana.
Prerequisite, done 2026-08-26: both source repos moved from the `doublezerofoundation`
org to `malbeclabs`, and every reference was repointed. The five pull requests that did it:
doublezero-offchain#412, doublezero-solana#125, doublezero-shreds#686, doublezero#4236,
docs#202.

## Why

Three problems, all of which come from the code living in three repos.

1. **A change that spans the three repos takes three pull requests in a fixed order.**
   It also takes a pin bump in each consumer. On 2026-08-26 this failed in a way that
   was hard to read: `doublezero-shreds` pointed `doublezero-program-tools` at the new
   org while the offchain revision it pinned still pointed at the old one. Cargo makes
   a git dependency's URL part of the crate identity, so it built two copies of the
   crate. The oracle then reported 194 errors of the form "no method named X", on types
   that plainly had X. Nothing in the error text pointed at the URL.
2. **Three release setups.** Offchain runs five goreleaser configs and its own Cloudsmith
   push. Solana runs none. This repo runs one per component already. Three repos means
   three sets of Actions secrets, three rulesets, three CODEOWNERS files.
3. **Version skew has no single source of truth.** Ten git dependencies carry pins today.
   Nothing checks that they agree.

## Scope

In scope: move `doublezero-offchain` and `doublezero-solana` into this repo, merge the
Cargo workspaces, and merge the release and CI setup.

Out of scope, decided deliberately:

- **`doublezero-shreds` stays a separate repo.** It is private and this repo is public.
  It keeps git dependencies and coordinated pin bumps. It does get a large improvement:
  three upstream pins collapse to one. See "What this does to shreds".
- **`network-shapley-rs` stays in the `doublezerofoundation` org.** It is public and it
  works. Moving it is a separate job for another day.
- **Consolidating the two sentinel crates.** This spec renames a binary to stop a
  collision. Whether the two crates should become one is a later question.
- **Any other malbeclabs repo.** The pattern here can be reused for a later wave.

## Prior art

An earlier attempt is tracked in malbeclabs/infra#1952 (offchain into the monorepo) and
malbeclabs/infra#1515 (folding the `doublezero-solana` CLI into the main binary). Both are
open and neither binds this spec, but they were read before writing it and they changed it.

Where they agree with this spec, independently: offchain lands under a **top-level
`offchain/`** rather than inside `crates/`, for the same reason (`crates/` is a flat bucket
of single crates; offchain is a multi-crate tree plus an Elixir component). The staged,
stop-anywhere shape is also the same.

Where this spec differs:

- **#1952 leaves `doublezero-solana` out of scope** as an external git dependency. This
  spec brings it in. The objection at the time was partly a cross-org governance one, and
  that is gone: both repos now sit in `malbeclabs`.
- **#1952 Phase 2a wanted the monorepo bumped to 1.92 plus a musl target.** Both are
  obsolete. The monorepo is now on 1.97.1, so the toolchain moves the other way (D3), and
  it already builds `x86_64-unknown-linux-musl` in `rust.yml`, `release.client.yml`,
  `release.daily.yml` and `release.pipeline.validation.yml`.

Two warnings from #1515 are carried into this spec, in D3 and in Risks. They were the most
valuable thing in either issue. Neither issue stalled for a technical reason, so nothing in
them was invalidated by discovery.

#1952 has five open, unassigned sub-issues. This is how they map, so nobody works from two
plans at once:

| Issue | Fate |
| --- | --- |
| #1947 Phase 0, import under `offchain/` | Superseded by step 1, which also imports solana |
| #1948 Phase 1, flip git deps to path deps | Folded into step 2 |
| #1949 Phase 2a, toolchain 1.92 plus musl | **Obsolete.** Monorepo is on 1.97.1 and already builds musl |
| #1950 Phase 2b, unify the workspace | Becomes step 2 |
| #1951 Phase 3, distribute crates and mount verbs | **Still valid, out of scope here, unblocked by step 2** |

Close #1947 through #1950 against this spec if it is accepted. Keep #1951 and #1515 open.

**Out of scope but unblocked:** #1515 wants a `doublezero solana <verb>` surface mounted in
the main binary. It notes that a shared `CliContext` only unifies inside one workspace,
which is why its plan needs reach-back and pin alignment. Step 2 of this spec removes that
constraint, so #1515's increments reduce to adding a path dependency and a subcommand. This
spec does not do that work.

## The state we are starting from

| | doublezero | offchain | solana |
| --- | --- | --- | --- |
| Rust crates | 22 | 16 | 6 |
| Rust lines | 199k | 52k | 21k |
| Go lines | 454k | 0 | 0 |
| Other | 2 Go modules | Elixir app + Rust NIF | none |
| Workflows | 49 | 8 | 3 |
| Toolchain | 1.97.1 | 1.92.0 | 1.91 |
| `solana-sdk` | 3.0.0 | 3.0.0 | 3.0.0 |
| `borsh` | 1.7.0 | 1.7.0 | 1.6.0 |

The Solana crates already agree across all three repos. Only borsh and the toolchain
differ, and both move upward without a break. The usual reason a monorepo merge stalls
is absent here.

Two facts about this repo make the merge cheaper than it looks:

- `smartcontract/programs/rust-toolchain.toml` already pins `channel = "1.91"`, the same
  channel doublezero-solana uses. Per-directory toolchain pinning is established practice
  here.
- The root `Cargo.toml` already carries an `exclude` list for nested workspaces (the four
  `generate-fixtures` directories). Nested workspaces are established practice too.

## Decisions

### D1. Two new top-level directories. Nothing existing moves.

```
doublezero/
├── crates/                     22 crates, unchanged
├── smartcontract/programs/     DoubleZero Ledger programs, unchanged
├── client/ sdk/ e2e/ ...       unchanged
│
├── solana/                     from doublezero-solana
│   ├── programs/passport
│   ├── programs/revenue-distribution
│   ├── crates/program-tools
│   └── mock/{swap-sol-2z,rewards-integration}
│
└── offchain/                   from doublezero-offchain
    ├── crates/                 14 crates
    └── scheduler/              Elixir app and its Rust NIF
```

This matches how the repo is already laid out: `client/`, `sdk/`, `smartcontract/`,
`controlplane/`, `e2e/` are all top-level trees named for what they hold.

Rejected: flattening offchain's crates into `crates/` and solana's programs into
`smartcontract/programs/`. Three reasons.

1. **The two program sets run on different chains.** This repo's own CLAUDE.md already
   documents the split: the serviceability, telemetry, geolocation and record programs
   deploy to the **DoubleZero Ledger** (`ledger_rpc_url`), while **Solana L1**
   (`solana_l1_rpc_url`) is a separate network carrying the 2Z token. `passport` and
   `revenue-distribution` deploy to Solana L1. The offchain scheduler's config carries
   `DZ_LEDGER_RPC` and `SOLANA_RPC` as separate endpoints for the same reason. Putting
   both sets in `smartcontract/programs/` would hide a distinction the codebase already
   treats as load-bearing, and one the CLAUDE.md warns fails silently when confused: a
   lookup against the wrong cluster does not error, the account simply is not there.
2. It puts 38 crates in one flat directory.
3. It forces the sentinel directory rename on day one. Keeping the trees separate makes
   that collision disappear (`offchain/crates/sentinel` next to `crates/sentinel`).

### D2. One workspace, with the two Solana programs held back until measured.

After the workspace merge, the root workspace gains the 14 offchain crates,
`scheduler/native/scheduler_doublezero`, and `solana/crates/program-tools`.

`passport` and `revenue-distribution` stay in `exclude` until we measure whether folding
them in changes the bytes they compile to. `doublezero-solana` builds them in Docker with
`cargo fetch --locked` and checks them against `programs/sha256sums_*.txt`. A shared
lockfile may resolve some dependency differently, which would change the artifact.

Step 0 below settles this by measurement, not by argument. If the bytes do not change,
they join the root workspace. If they change, they stay excluded with their own lockfile,
and they keep needing a pin bump like today.

The two `mock/` programs stay excluded. Offchain already excludes its own mock program,
so this follows the existing convention.

### D3. Toolchain: root stays 1.97.1, programs pin 1.91.

Add `solana/programs/rust-toolchain.toml` pinning 1.91, mirroring
`smartcontract/programs/rust-toolchain.toml`.

Offchain's repo-wide 1.92.0 pin goes away and its crates build on 1.97.1. Expect new
clippy findings. That work belongs to step 2 and is not a surprise to discover later.

borsh unifies on 1.7.0.

**The edition trap, from #1515.** This repo's `[workspace.package]` sets
`edition = "2021"`. Offchain's sets `"2024"`. All **14** offchain crates declare
`edition.workspace = true`, so folding them into the root workspace silently moves every
one of them from 2024 to 2021, and they fail to build.

Fix: set `edition = "2024"` explicitly on all 14 crates in step 2. One line each. If this
repo later moves its workspace to 2024, those lines drop out.

Rejected: bumping this repo's workspace edition to 2024 as part of the merge. It would
touch all 22 existing crates and put an unrelated migration inside the step that already
carries the most risk.

### D4. Rename this repo's sentinel binary.

Both repos have a `crates/sentinel`, and both declare `[[bin]] name = "doublezero-sentinel"`.
Two members of one workspace writing the same file into `target/release/` is a real
collision.

| | this repo | offchain |
| --- | --- | --- |
| package | `doublezero-sentinel` | `doublezero-ledger-sentinel` |
| binary | `doublezero-sentinel` | `doublezero-sentinel` |
| released? | no | yes, as a Cloudsmith deb |
| used by | `e2e.yml` only | deployed |

Rename this repo's binary to `dz-e2e-sentinel`. It has no external contract: no goreleaser
config, gated behind `required-features = ["server"]`, and built only into the
`ghcr.io/malbeclabs/dz-e2e/sentinel` image that the e2e shards consume. Offchain's keeps
its name because `doublezero-sentinel` is a deployed package name.

The rename touches two files: `crates/sentinel/Cargo.toml` and
`e2e/docker/sentinel/Dockerfile`.

### D5. Keep the imported tags, unprefixed. Retire one orphan first.

`git filter-repo` carries tags by default. Keep them: they make release history reachable
from this repo, and an unprefixed tag continues each component's version line, so a later
release of `contributor-rewards` picks up from `v0.6.1` rather than restarting.

Unprefixed is safe because there is almost no overlap. Comparing every tag prefix across
the three repos, 33 in this repo, 14 in offchain, 2 in solana, gives exactly one collision:

```
sentinel
```

**That collision resolves by deleting the orphan, which we want to do anyway.** This repo
holds a single `sentinel/v0.6.1` tag with no sentinel release behind it: no goreleaser
config, no published artifact. Offchain's sentinel line is the live one and reaches
`v0.2.6`, so its next release is `sentinel/v0.2.7`, below the orphan. Left in place the
version line reads backwards, and a later release at v0.6.1 fails on an existing tag.

So: **delete `sentinel/v0.6.1` before the import**, then import every tag unprefixed. No
`--tag-rename`, no long names, and every component keeps one continuous version line.

Check the collision set again immediately before importing, in case new tags have landed:

```sh
comm -12 <(gh api 'repos/malbeclabs/doublezero-offchain/tags?per_page=100' --paginate \
            -q '.[].name' | sed -E 's|/v[0-9].*$||' | sort -u) \
         <(gh api 'repos/malbeclabs/doublezero/tags?per_page=100' --paginate \
            -q '.[].name' | sed -E 's|/v[0-9].*$||' | sort -u)
```

Anything it prints needs a decision before step 1 runs.

## Sequencing

One throwaway measurement, then four pull requests in this repo, then one follow-on
pull request in `doublezero-shreds`.

### Step 0. Measure the program bytes. Throwaway.

Do the whole merge locally. Add `passport` and `revenue-distribution` as root workspace
members. Rebuild through the existing path:

```sh
make build-artifacts NETWORK=mainnet-beta
shasum -a 256 -c programs/sha256sums_mainnet_beta.txt
```

Repeat for `NETWORK=development`. Output is one fact that decides D2. Discard the work
either way.

### Step 1. Import the code and the history.

First delete the orphan `sentinel/v0.6.1` tag per D5, then re-run the collision check
in D5 and resolve anything it prints. Then run `git filter-repo --path-rename` on each
source repo to relocate its tree, keeping tags unprefixed. Add each as a remote and merge with
`--allow-unrelated-histories`. Fetch `main` only. Offchain alone has 599 refs and none of
the others are wanted.

Add `offchain/` and `solana/` to the root `exclude` list wholesale. Both trees keep their
own `Cargo.toml` and `Cargo.lock` and build as nested workspaces, exactly as they do
today.

Do not bring their `.github/workflows/` across yet. Eleven workflows firing on a repo
that does not expect them is noise.

Gate: every existing job green, with no existing job definition touched. `git diff`
between each imported tree and its filtered source is empty. The diff is large and needs
no judgement to review.

### Step 2. Merge the workspaces.

This is the step that can hurt, and it hurts alone.

- Root workspace absorbs the crates per D2.
- Delete the four nested `Cargo.toml` and `Cargo.lock` files.
- **Ten git dependencies become path dependencies.** Offchain's seven
  `malbeclabs/doublezero` dependencies and its three `doublezero-solana` dependencies are
  all same-repo now. The pinning that failed on 2026-08-26 stops existing.
- Apply D3 (toolchain, borsh) and D4 (sentinel rename).

Gate: `cargo check --workspace`, `cargo clippy --all-targets`, the full test suite, e2e,
and the program checksum check. Also confirm the lockfile holds no crate twice:
`grep '^name = ' Cargo.lock | sort | uniq -d` should print nothing that was not already
duplicated before the merge.

### Step 3. Merge release and CI.

Fold 11 workflows into this repo's 49. This repo already runs one release workflow per
component, so offchain's five fit the existing shape.

- Dedupe the overlaps: two `local-validator.yml`, three Rust CI configs (`rust.yml` twice
  plus offchain's `ci.yml`), two `changelog-reminder.yml`.
- Add `erlef/setup-beam` for the Elixir scheduler.
- Copy offchain's three release secrets onto this repo: `CLOUDSMITH_TOKEN`,
  `GORELEASER_KEY`, `SLACK_BOTS_WEBHOOK`.
- **Change `release.github.name` in the five goreleaser configs from `doublezero-offchain`
  to `doublezero`.** The `owner` field was corrected during the org move. The `name` field
  becomes wrong the moment the code lives here.

Gate: push a throwaway release candidate tag on the smallest component. Confirm the
release lands under malbeclabs and the package reaches Cloudsmith. Config review proves
the target is named correctly. It does not prove the token can write there.

### Step 4. Decommission the source repos.

Archive both read-only with a README pointing here. Do not delete them. Their transfer
redirects, 11 forks, and old release download URLs all still resolve through them.

Archiving also keeps shreds building on its current pins. An archived repo still serves
reads, so `malbeclabs/doublezero-solana` at tag `revenue-distribution/v0.3.7` keeps
resolving, and the same tag imported here points at the same commit. That decouples step 5
from step 4: shreds can be repointed when it suits, not the same day.

### Step 5. Repoint shreds. Separate repo, separate risk.

`doublezero-shreds` consumes both source repos over git. Once they are archived it must
point at `malbeclabs/doublezero`. See below for why this needs care.

## What this does to shreds

Today shreds pins three repos at three refs:

```
shreds → malbeclabs/doublezero            rev 8bb7900e                      2 crates
shreds → malbeclabs/doublezero-offchain   rev 0c84bdef                      1 crate
shreds → malbeclabs/doublezero-solana     tag revenue-distribution/v0.3.7   2 crates
```

Shreds declares `doublezero_sdk` at rev `8bb7900e` and also inherits it at tag
`client/v0.31.0` through offchain. Two URLs for one crate, so two copies are built. It
compiles today only because those types never meet.

After this migration, everything shreds needs lives in `malbeclabs/doublezero`: the sdk,
serviceability, solana-client-tools, program-tools and revenue-distribution. One URL, one
ref, one copy of each crate. The duplicate cannot survive, because no second source
exists for it to come from.

That holds on one condition. **Shreds must repoint every `malbeclabs/doublezero`
dependency to a single ref in one commit.** A half-repoint reproduces the 2026-08-26
failure exactly.

Give the shreds change a real check rather than "it builds":

```sh
grep -oE 'git\+https://github.com/malbeclabs/doublezero[^"]*' Cargo.lock | sort -u   # expect 1 line
grep -c 'name = "doublezero_sdk"' Cargo.lock                                          # expect 1
```

Keep that as a CI guard in shreds afterwards. It fails the moment someone reintroduces a
second ref, which is far cheaper than diagnosing 194 errors again.

Shreds still needs coordinated pin bumps to pick up changes from this repo. One pin
instead of three is a real gain. Full atomic change only reaches code inside this repo.
That is the price of the public and private split, and we are paying it on purpose.

## Rollback

Steps 2, 3 and 4 revert cleanly.

Step 2 reverts cleanly **because step 1 deliberately touches no build file.** Step 2 only
edits and deletes manifests, so reverting it restores the four nested `Cargo.toml` and
`Cargo.lock` files and leaves a repo that builds as it did after step 1. Step 3 is
workflows. Unarchiving a repo is a click.

Step 1 is the one-way door. `git revert -m 1` removes the files, and the grafted history
stays in the graph unless `main` is rewritten. Treat merging step 1 as the commitment
point, not step 2.

## Risks

**The 68 branches.** `doublezero-shreds` has 68 remote branches carrying
`programs/Cargo.toml` with old org URLs. They are not on `main`. Each one merged after
the org move reintroduces an old URL. Harmless while the transfer redirects hold, and a
build failure once they do not. Rebasing them is shreds work, not this migration's, but it
interacts with step 5.

**Clippy churn on the toolchain bump.** Offchain moves from 1.92.0 to 1.97.1 in step 2.
Volume is unknown until tried. Step 0 can measure it at the same time as the program
bytes, for free.

**Contributor reward output can change silently. This is the risk to take seriously.**
#1515 flags it for both `contributor-rewards` and `validator-debt`: relocating them
re-resolves their maths in a different lockfile and feature context, and the output can
change without anything failing. Both move into the root workspace in step 2.

Only `contributor-rewards` is load-bearing. Validator debt is no longer being collected, so
drift in `validator-debt` output changes no payment. It still has to compile and it still
releases, but it does not need a correctness gate.

`contributor-rewards` decides what contributors are paid. It has **no golden or snapshot
tests** today, so nothing would catch a change. The failure mode is wrong reward figures
that build clean and pass every test.

Mitigation, and it should land before step 2 rather than inside it: add golden tests to
`contributor-rewards` on `main` as it stands now. Fixed input, byte-identical output. Then
step 2 either keeps them green or names exactly what moved. Writing goldens against today's
behaviour is worth doing on its own merits, whatever happens to this migration.

**Is `validator-debt` worth migrating at all?** If it is not collecting and is not expected
to, moving it, releasing it and carrying it in the workspace is work spent on dormant code.
Deleting it, or archiving it in place, may be cheaper than migrating it. Worth a decision
before step 2 rather than after. Note it is still deployed: `malbeclabs/infra` runs it from
the offchain scheduler with its own AWS credentials, and it ships a Cloudsmith package, so
this is a real decision and not a formality.

**One lockfile, one resolution.** Today three lockfiles disagreeing is a visible signal.
After step 2 one lockfile resolves silently. The Solana crates agree today only because
of an exact pin (`solana-sdk = "=3.0"` in all three). Keep that pin.

**The fixture generators float.** `sdk/revdist/testdata/fixtures/generate-fixtures`
declares its two `doublezero-solana` dependencies with no tag and no rev. Only the
lockfile pins them, so the next `cargo update` there moves them to whatever `main` holds.
Pin them during step 2.

**`doublezero-solana` uses version ranges** (`>=2,<=3`) on its program-side crates. That
earns its keep for a library other repos consume. Inside one workspace it only produces
resolution nobody asked for. Convert to exact pins during step 2.
