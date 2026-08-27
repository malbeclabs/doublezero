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
| #1947 Phase 0, import under `offchain/` | Superseded by step 2, which also imports solana |
| #1948 Phase 1, flip git deps to path deps | Becomes step 3 |
| #1949 Phase 2a, toolchain 1.92 plus musl | **Obsolete.** Monorepo is on 1.97.1 and already builds musl |
| #1950 Phase 2b, unify the workspace | Becomes step 4 |
| #1951 Phase 3, distribute crates and mount verbs | **Still valid, out of scope here, unblocked by step 4** |

Close #1947 through #1950 against this spec if it is accepted. Keep #1951 and #1515 open.

**Out of scope but unblocked:** #1515 wants a `doublezero solana <verb>` surface mounted in
the main binary. It notes that a shared `CliContext` only unifies inside one workspace,
which is why its plan needs reach-back and pin alignment. Step 4 of this spec removes that
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
│   │                           NESTED WORKSPACE, excluded from root (D2)
│   │                           keeps its own Cargo.lock + rust-toolchain.toml
│   ├── programs/passport
│   ├── programs/revenue-distribution
│   ├── crates/program-tools
│   └── mock/{swap-sol-2z,rewards-integration}
│
└── offchain/                   from doublezero-offchain
    │                           root workspace members after step 4
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

### D2. Offchain joins the root workspace. The `solana/` tree stays nested. Measured.

**This was open pending step 0. Step 0 has run and the answer is no: the two Solana
programs must not join the root workspace.**

The programs' build closure is 96 crates. **62 of them resolve to a different version in
this repo's lockfile.** A sample:

| crate | in solana | in this repo |
| --- | --- | --- |
| `borsh` | 1.6.0 | 1.7.0 |
| `bytemuck` | 1.24.0 | 1.25.0 |
| `solana-instruction` | 3.1.0 | 3.4.0 |
| `solana-clock` | 3.0.0 | 3.1.0 |
| `solana-account-info` | 3.1.0 | 3.1.1 |
| `solana-fee-calculator` | 3.0.0 | 3.2.1 |

`borsh` and `bytemuck` are the serialization and zero-copy layout crates the account
structs are built on. A shared lockfile changes the compiled bytes, so
`programs/sha256sums_*.txt` would no longer verify and the deployed artifacts would stop
being reproducible from this repo.

So the end state is:

- **`offchain/` joins the root workspace.** Its 14 crates plus
  `scheduler/native/scheduler_doublezero` become members.
- **`solana/` stays a nested excluded workspace**, keeping its own `Cargo.toml`,
  `Cargo.lock` and `rust-toolchain.toml`. That covers `programs/passport`,
  `programs/revenue-distribution`, `crates/program-tools` and both `mock/` programs.
- **Offchain reaches `program-tools` by path** into that excluded tree, rather than by git
  tag. `program-tools` compiles twice, once per lockfile, exactly as it does today.

**This does not cost the atomic-change goal.** The pin disappears either way. A change
touching `program-tools` and offchain together is still one commit in one repo, reviewed
and merged atomically. Only dependency *resolution* stays separate, which is the whole
point of holding the programs back.

It also makes step 4 smaller than planned: it merges offchain only.

Re-measure if this repo's lockfile ever converges on solana's versions, using the method in
step 0. Nothing about this is permanent.

### D3. Toolchain: root stays 1.97.1, the Solana programs keep 1.91.

Nothing to add. `solana/` stays a nested workspace per D2, so its existing repo-wide
`rust-toolchain.toml` (channel 1.91) carries over and keeps applying to that tree. That is
the same directory-scoped pinning this repo already uses at
`smartcontract/programs/rust-toolchain.toml`.

Offchain's repo-wide 1.92.0 pin goes away and its crates build on 1.97.1. **Measured in
step 0: it compiles clean with 15 clippy warnings**, all trivial and mostly `--fix`-able
(`useless_conversion`, `useless_borrows_in_formatting`, `unnecessary_sort_by`,
`explicit_counter_loop`, `collapsible_match`). This was expected to be the painful part of
step 4. It is not.

borsh unifies on 1.7.0.

**The edition trap, from #1515.** This repo's `[workspace.package]` sets
`edition = "2021"`. Offchain's sets `"2024"`. All **14** offchain crates declare
`edition.workspace = true`, so folding them into the root workspace silently moves every
one of them from 2024 to 2021, and they fail to build.

Fix: set `edition = "2024"` explicitly on all 14 crates in step 4. One line each. If this
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

Anything it prints needs a decision before step 2 runs.

## Two cargo behaviours this plan depends on

Both were tested with a minimal reproduction rather than reasoned about, because the design
rests on them.

1. **A root workspace can path-depend into an excluded nested workspace.** So offchain, as
   a root member, can depend on `solana/crates/program-tools` by path while `solana/` keeps
   its own workspace and lockfile. Verified: builds clean.
2. **A git dependency can reach a package in the repo's `exclude` list.** Cargo scans the
   repository for the named package; workspace membership does not gate git dependencies.
   Verified: builds clean.

The second one matters for shreds. Holding `revenue-distribution` out of the root workspace
does **not** stop shreds pulling it from `malbeclabs/doublezero` by git, so the single-source
story in "What this does to shreds" survives D2 intact.

## Sequencing

One throwaway measurement, then six pull requests in this repo, then one follow-on pull
request in `doublezero-shreds`.

The riskiest work is split across steps 3 and 4 so that flipping the dependencies and
merging the workspaces fail separately. Each step is revertable on its own except step 2,
which is the one-way door.

### Step 0. Measure whether a shared lockfile changes the program bytes. DONE.

**Method.** Rebuilding the artifacts is the wrong test, and the obvious version of it gives
a false negative: this repo has no `build-artifacts` target, and solana's Docker path runs
`cargo fetch --locked` against solana's *own* lockfile, so it would report no change however
the merged workspace resolved.

The bytes can only change if the resolved dependency versions change, so compare resolution
directly. Take the programs' normal-dependency closure, then compare each crate's resolved
version against this repo's lockfile:

```sh
cargo tree --locked -p doublezero-passport -e normal --prefix none --features entrypoint \
  | sed 's/ (\*)$//' | awk 'NF{print $1" "$2}' | sort -u
```

Cheaper than a build, exact, and it names *which* crates move rather than just saying the
hash differs.

**Result.** 96 crates in the closure, **62 resolve differently**. See D2. The programs stay
excluded.

**Also measured, since the tree was already built:** offchain on 1.97.1 compiles clean with
15 trivial clippy warnings. See D3.

**Prerequisite this surfaced:** `git-filter-repo` is not installed. Step 2 needs it.
Step 0 did not, because dependency resolution depends on manifests and paths, not history.

### Step 1. Golden tests for `contributor-rewards`.

Land on `main`, before anything moves. Fixed input, byte-identical reward output. This crate
decides what contributors are paid and has no output test today, so step 4 currently has no
way to prove it changed nothing.

This step stands on its own merits and is worth landing whether or not the rest of this
spec proceeds.

`validator-debt` does not need the same gate. Debt is not being collected, so drift in its
output changes no payment. It still has to compile and release, and it stays in the tree.

Gate: the goldens pass on `main` and fail if a reward figure moves.

### Step 2. Import the code and the history. One-way door.

First delete the orphan `sentinel/v0.6.1` tag per D5, then re-run the collision check in D5
and resolve anything it prints. Then run `git filter-repo --path-rename` on each source
repo to relocate its tree, keeping tags unprefixed. Add each as a remote and merge with
`--allow-unrelated-histories`. Fetch `main` only. Offchain alone has 599 refs and none of
the others are wanted.

Add `offchain/` and `solana/` to the root `exclude` list wholesale. Both trees keep their
own `Cargo.toml` and `Cargo.lock` and build as nested workspaces, exactly as they do
today.

Do not bring their `.github/workflows/` across yet. Eleven workflows firing on a repo that
does not expect them is noise.

Gate: every existing job green, with no existing job definition touched. `git diff` between
each imported tree and its filtered source is empty. The diff is large and needs no
judgment to review.

### Step 3. Flip the git dependencies to path dependencies.

Both trees are still excluded nested workspaces. A path dependency may point outside its
own workspace, so this works before the workspaces merge, and it delivers most of the value
on its own.

- Offchain's 7 `malbeclabs/doublezero` git deps become paths into `crates/`, `client/`,
  `config/` and `smartcontract/`.
- Offchain's 3 `doublezero-solana` git deps become paths into `solana/`.
- **All ten pins stop existing.** The class of failure that produced 194 errors on
  2026-08-26 is gone from this repo at the end of this step.

Nothing else changes. Toolchains, editions, borsh and the sentinel binary name are all
untouched, and both nested lockfiles stay in place.

Gate: both nested workspaces build and test standalone. No `git+` source for
`malbeclabs/doublezero` or `malbeclabs/doublezero-solana` remains in either nested
lockfile.

### Step 4. Merge the workspaces.

Now a manifest consolidation plus the toolchain work, with the dependency flip already
proven by step 3.

- Root workspace absorbs **offchain's** crates per D2.
- Delete `offchain/Cargo.toml`, `offchain/Cargo.lock` and `offchain/rust-toolchain.toml`.
  A `rust-toolchain.toml` is directory-scoped, so leaving it would give a different compiler
  depending on which directory cargo was invoked from.
- **Keep `solana/Cargo.toml`, `solana/Cargo.lock` and `solana/rust-toolchain.toml`.** The
  programs stay excluded per D2 and need their own locked build for the checksum gate. Do
  not delete them.
- Set `edition = "2024"` explicitly on all 14 offchain crates per D3.
- Offchain's crates move to 1.97.1; borsh unifies on 1.7.0; rename the sentinel binary
  per D4.
- Pin the fixture generators and convert solana's `>=2,<=3` ranges to exact pins.

Gate: `cargo check --workspace`, `cargo clippy --all-targets`, the full test suite, e2e,
the program checksum check from inside `solana/`, and **the step 1 goldens still green**.

Two lockfile gates. A bare duplicate-name check is useless here: `cargo tree -d --workspace`
already reports 140 duplicate groups on `main` today, which are normal.

```sh
# no internal git sources should remain; all are path deps after step 3
grep -c 'source = "git+https://github.com/malbeclabs' Cargo.lock    # expect 0

# duplicate groups must not increase against the pre-merge baseline
cargo tree -d --workspace --locked | grep -cE '^[a-z0-9_-]+ v'      # baseline was 140
```

### Step 5. Merge release and CI.

Fold 11 workflows into this repo's 49. This repo already runs one release workflow per
component, so offchain's five fit the existing shape.

- Dedupe the overlaps: two `local-validator.yml`, three Rust CI configs (`rust.yml` twice
  plus offchain's `ci.yml`), two `changelog-reminder.yml`.
- Add `erlef/setup-beam` for the Elixir scheduler, with CI path-scoped to
  `offchain/scheduler/**` so it does not run on unrelated changes.
- Copy offchain's three release secrets onto this repo: `CLOUDSMITH_TOKEN`,
  `GORELEASER_KEY`, `SLACK_BOTS_WEBHOOK`.
- **Change `release.github.name` in the five goreleaser configs from `doublezero-offchain`
  to `doublezero`.** The `owner` field was corrected during the org move. The `name` field
  becomes wrong the moment the code lives here.

Gate: push a throwaway release candidate tag on the smallest component. Confirm the release
lands under malbeclabs and the package reaches Cloudsmith. Config review proves the target
is named correctly. It does not prove the token can write there.

### Step 6. Decommission the source repos.

Archive both read-only with a README pointing here. Do not delete them. Their transfer
redirects, 11 forks, and old release download URLs all still resolve through them.

Archiving also keeps shreds building on its current pins. An archived repo still serves
reads, so `malbeclabs/doublezero-solana` at tag `revenue-distribution/v0.3.7` keeps
resolving, and the same tag imported here points at the same commit. That decouples step 7
from step 6: shreds can be repointed when it suits, not the same day.

### Step 7. Repoint shreds. Separate repo, separate risk.

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

Every step reverts cleanly except step 2.

Step 3 reverts to git dependencies. Step 4 reverts **because steps 2 and 3 leave the nested
manifests in place**: step 4 only edits and deletes manifests, so reverting it restores the
`offchain/Cargo.toml` and `offchain/Cargo.lock` and leaves a repo that builds as it did
after step 3. Step 5 is workflows. Unarchiving a repo is a click.

Splitting the old single workspace step into steps 3 and 4 is what buys this. A revert of
step 4 no longer drags the dependency flip back with it.

Step 2 is the one-way door. `git revert -m 1` removes the files, and the grafted history
stays in the graph unless `main` is rewritten. Treat merging step 2 as the commitment
point.

## Risks

**The 68 branches.** `doublezero-shreds` has 68 remote branches carrying
`programs/Cargo.toml` with old org URLs. They are not on `main`. Each one merged after
the org move reintroduces an old URL. Harmless while the transfer redirects hold, and a
build failure once they do not. Rebasing them is shreds work, not this migration's, but it
interacts with step 7.

**Clippy churn on the toolchain bump. Measured and small.** Offchain moves from 1.92.0 to
1.97.1 in step 4. Step 0 ran it: clean compile, 15 trivial warnings, mostly `--fix`-able.
This risk is closed.

**Contributor reward output can change silently. This is the risk to take seriously.**
#1515 flags it: relocating `contributor-rewards` re-resolves its maths in a different
lockfile and feature context, and the output can change without anything failing. It moves
into the root workspace in step 4.

`contributor-rewards` decides what contributors are paid, and it has **no golden or
snapshot tests** today. The failure mode is wrong reward figures that build clean and pass
every test.

This is why step 1 exists and why it comes first: write the goldens against today's
behaviour on `main`, before anything moves. Step 4 then either keeps them green or names
exactly what changed.

**`validator-debt` stays, without a correctness gate.** It is not collecting today, so
drift in its output changes no payment. It is kept deliberately rather than dropped: it is
still deployed, `malbeclabs/infra` runs it from the offchain scheduler with its own AWS
credentials, and it ships a Cloudsmith package. It migrates and keeps releasing like
everything else.

**One lockfile, one resolution.** Today three lockfiles disagreeing is a visible signal.
After step 4 one lockfile resolves silently. The Solana crates agree today only because
of an exact pin (`solana-sdk = "=3.0"` in all three). Keep that pin.

**The fixture generators float.** `sdk/revdist/testdata/fixtures/generate-fixtures`
declares its two `doublezero-solana` dependencies with no tag and no rev. Only the
lockfile pins them, so the next `cargo update` there moves them to whatever `main` holds.
Pin them during step 4.

**`doublezero-solana` uses version ranges** (`>=2,<=3`) on its program-side crates. That
earns its keep for a library other repos consume. Inside one workspace it only produces
resolution nobody asked for. Convert to exact pins during step 4.
