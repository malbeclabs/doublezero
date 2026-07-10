# Testnet Release Runbook

The testnet release is driven by a single orchestrator workflow,
[`release.testnet.yml`](../.github/workflows/release.testnet.yml). It automates
everything that can be automated and pauses at two human gates: one before tags
are pushed, and one around the Solana program deploy (which stays manual).

Beyond the two gates, several jobs run in approval-protected environments, so a
full release is roughly 6–7 approval interactions across the two repos: merge
both version PRs, approve gate 1, approve the `testnet` environment on the tag
jobs, approve infra's `testnet` environment three times (`stage-programs`,
`deploy-core`, `deploy-clients` — each dispatched infra run posts a link to
`#bots` when it may be waiting), and approve gate 2 after the program
deploy.

## Slack notifications

All Slack traffic for a run lands in a single `#bots` thread: `preflight`
posts the parent message ("Testnet Deploy vX.Y.Z :thread:") after its
validation steps pass, and every later post — PR links, the tag-approval nudge,
approval pings for the dispatched infra runs, the program-deploy call, success,
failure — is a reply in that thread. Posts go through the Slack Web API (`chat.postMessage`) using
the `SLACK_BOT_TOKEN` repo secret and the channel ID set in the workflow's
`SLACK_CHANNEL_ID` env var.

Slack delivery is best-effort by design: a failed post (outage, missing or bad
token) emits a workflow warning and never fails or blocks the release. If the
parent post itself failed, downstream posts degrade to top-level (unthreaded)
`#bots` messages.

Re-run semantics: "Re-run failed jobs" keeps `preflight`'s outputs, so posts
continue in the same thread. "Re-run all jobs" re-runs `preflight` and starts
a new thread.

## Starting a release

```bash
gh workflow run release.testnet.yml -R malbeclabs/doublezero -f version=X.Y.Z
```

`version` is plain `X.Y.Z` with no leading `v`. Optional inputs:

| Input | Default | Effect |
| --- | --- | --- |
| `dry_run` | `false` | Validate plumbing end to end: version PRs are opened as drafts, no tags are pushed, downstream deploys run in check mode, the onchain check is skipped. |
| `skip_devnet_check` | `false` | Skip the preflight check that the latest devnet daily release succeeded. Use only when you know why devnet is red. |

## Stage by stage

| Stage | Automated | Human action required |
| --- | --- | --- |
| `preflight` | Validates the version, reads the current workspace version, checks the latest devnet daily release succeeded. | — |
| `open-prs` | Opens the doublezero version-bump PR (`release/vX.Y.Z`: Cargo.toml, Cargo.lock, CHANGELOG promotion) and the infra pinned-versions PR (`release/testnet-vX.Y.Z`). PR links appear in the run summary and are posted to the Slack thread. | Review and **merge both PRs**. |
| `gate-tags` | Waits on the `testnet` environment, then verifies both PRs are merged (the gate fails if you approve early). | **Approve gate 1** after both PRs are merged. |
| `push-tags` | Pushes the 9 component tags (`controller`, `internet-latency-collector`, `agent`, `device-telemetry-agent`, `geoprobe-agent`, `geoprobe-target`, `funder`, `monitor`, `client`) via the reusable tag workflow, which runs in the protected `testnet` environment. | **Approve the `testnet` environment prompt** on the tag jobs (nudged in the Slack thread). |
| `verify-cloudsmith` | Polls CloudSmith (up to ~60 min) until all 9 packages exist at the new version. | — |
| `build-programs` | Builds the three Solana programs (`serviceability` default features; `telemetry` and `geolocation` with `--features testnet`) from main and uploads them with checksums and a `DEPLOY.md` manifest. | — |
| `stage-programs` | Dispatches the infra `stage-programs.testnet.yml` workflow, which copies the artifacts to `nyc-tn-bm2:/opt/doublezero/program-releases/vX.Y.Z/`, then pings Slack. | **Approve infra's `testnet` environment** on the dispatched run (link posted to `#bots`). Then **deploy the programs** on nyc-tn-bm2 following the `DEPLOY.md` staged alongside them (commands mirror the [infra runbook](https://github.com/malbeclabs/infra/blob/main/docs/runbooks/deploys/solana-programs-testnet.md)), and refresh the onchain version (`doublezero init`). |
| `gate-programs` | Waits on the `testnet` environment. | **Approve gate 2** once the programs are deployed and the onchain version is set. |
| `verify-onchain` | Installs the released client from CloudSmith and checks `doublezero --env testnet version` reports the new program version. | — |
| `deploy-core` | Dispatches infra `deploy-core.testnet.yml` and waits for it. | **Approve infra's `testnet` environment** on the dispatched run (link posted to `#bots`). |
| `deploy-clients` | Dispatches infra `deploy-clients.testnet.yml` and waits for it. | **Approve infra's `testnet` environment** on the dispatched run (link posted to `#bots`). |
| `qa` | Dispatches infra `qa.testnet.yml` and waits for it (may queue behind the hourly cron run). | — |
| `announce` | Posts success to Slack with the dashboard link. | **Watch the system dashboard for ~30 min** (https://doublezero.grafana.net/d/bf3dece9-51ac-4087-b6b1-579b3859ce14/). The foundation posts the community announcement. |

Any failed job triggers a Slack alert via `notify-failure`, posted to the same
thread.

All doublezero-side approvals (gate 1, the tag jobs, gate 2) use the single `testnet`
environment, so the approval prompts look alike — the review dialog lists which job(s)
are waiting; check the job name to know which gate you are approving. Each gate is
followed by a verification step, so approving the wrong thing fails fast rather than
advancing the release.

## Dry-run mode

`dry_run=true` exercises the plumbing without releasing anything:

- Both version PRs are opened as **drafts** with `[DRY RUN]` titles. Do not merge them;
  gate 1 only checks that they were not closed.
- No tags are pushed. `verify-cloudsmith` still runs, querying the **current** (previous)
  version to exercise the CloudSmith query logic.
- Programs are still built and staged, but `verify-onchain` is skipped, and the infra
  deploy workflows are dispatched in their check mode (`mode=dry-run`).
- The approval prompts are **not** skipped: gate 1, gate 2, the `testnet` environment
  on this repo, and infra's `testnet` environment all still require approval even
  though nothing is deployed.

Cleanup after a dry run: close both draft PRs and delete their branches
(`release/vX.Y.Z` in doublezero, `release/testnet-vX.Y.Z` in infra).

## Recovery / re-running

After a mid-pipeline failure, use **"Re-run all jobs"** (or dispatch a fresh run) —
**not "Re-run failed jobs"**. When a job fails, everything downstream of it is marked
skipped; "Re-run failed jobs" revives only the failed job and instantly re-marks the
previously-skipped jobs as skipped again, so the run can conclude "success" without
ever running the deploys, QA, or announce (observed on run 29106365876: the fixed
`stage-programs` job passed on re-run, and `gate-programs` through `announce` were
all carried over as skipped).

Full re-runs are safe by design:

- The version PRs are reused if they already exist (the branch is force-pushed and the
  open PR is found by head branch).
- Already-pushed tags are skipped (`skip_existing=true` on the tag workflow), so a
  partially completed tag matrix is safe to re-run.
- Program staging re-copies the same artifacts; environment gates prompt again.

If a dispatched infra workflow failed, fix the cause there first — the orchestrator
dispatches those workflows fresh from infra `main` at runtime, so infra-side fixes
apply on the next re-run without any doublezero change. Fixes to
`release.testnet.yml` itself always need a fresh dispatch: any re-run (failed or all)
executes the workflow snapshot from the original dispatch.
