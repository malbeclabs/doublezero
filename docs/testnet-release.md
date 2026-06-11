# Testnet Release Runbook

The testnet release is driven by a single orchestrator workflow,
[`release.testnet.yml`](../.github/workflows/release.testnet.yml). It automates
everything that can be automated and pauses at two human gates: one before tags
are pushed, and one around the Solana program deploy (which stays manual).

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
| `open-prs` | Opens the doublezero version-bump PR (`release/vX.Y.Z`: Cargo.toml, Cargo.lock, CHANGELOG promotion) and the infra pinned-versions PR (`release/testnet-vX.Y.Z`). PR links appear in the run summary. | Review and **merge both PRs**. |
| `gate-tags` | Waits on the `testnet-release-gate` environment, then verifies both PRs are merged (the gate fails if you approve early). | **Approve gate 1** after both PRs are merged. |
| `push-tags` | Pushes the 9 component tags (`controller`, `internet-latency-collector`, `agent`, `device-telemetry-agent`, `geoprobe-agent`, `geoprobe-target`, `funder`, `monitor`, `client`) via the reusable tag workflow, which runs in the protected `testnet` environment. | **Approve the `testnet` environment prompt** on the tag jobs. |
| `verify-cloudsmith` | Polls CloudSmith (up to ~60 min) until all 9 packages exist at the new version. | — |
| `build-programs` | Builds the three Solana programs (`serviceability` default features; `telemetry` and `geolocation` with `--features testnet`) from main and uploads them with checksums and a `DEPLOY.md` manifest. | — |
| `stage-programs` | Dispatches the infra `stage-programs.testnet.yml` workflow, which copies the artifacts to `nyc-tn-bm2:/opt/doublezero/program-releases/vX.Y.Z/`, then pings Slack. | **Deploy the programs** on nyc-tn-bm2 from that directory per the Notion runbook ("Build and deploy DoubleZero solana programs - testnet"), and set the onchain version. |
| `gate-programs` | Waits on the `testnet-program-deploy` environment. | **Approve gate 2** once the programs are deployed and the onchain version is set. |
| `verify-onchain` | Installs the released client from CloudSmith and checks `doublezero --env testnet version` reports the new program version. | — |
| `deploy-core` | Dispatches infra `deploy-core.testnet.yml` and waits for it. | — |
| `deploy-clients` | Dispatches infra `deploy-clients.testnet.yml` and waits for it. | — |
| `qa` | Dispatches infra `qa.testnet.yml` and waits for it (may queue behind the hourly cron run). | — |
| `announce` | Posts success to Slack with the dashboard link. | **Watch the system dashboard for ~30 min** (https://doublezero.grafana.net/d/bf3dece9-51ac-4087-b6b1-579b3859ce14/). The foundation posts the community announcement. |

Any failed job triggers a Slack alert via `notify-failure`.

## Dry-run mode

`dry_run=true` exercises the plumbing without releasing anything:

- Both version PRs are opened as **drafts** with `[DRY RUN]` titles. Do not merge them;
  gate 1 only checks that they were not closed.
- No tags are pushed. `verify-cloudsmith` still runs, querying the **current** (previous)
  version to exercise the CloudSmith query logic.
- Programs are still built and staged, but `verify-onchain` is skipped, and the infra
  deploy workflows are dispatched in their check mode (`mode=dry-run`).

Cleanup after a dry run: close both draft PRs and delete their branches
(`release/vX.Y.Z` in doublezero, `release/testnet-vX.Y.Z` in infra).

## Recovery / re-running

Use "Re-run failed jobs" on the orchestrator run to resume from where it stopped:

- The version PRs are reused if they already exist (the branch is force-pushed and the
  open PR is found by head branch).
- Already-pushed tags are skipped (`skip_existing=true` on the tag workflow), so a
  partially completed tag matrix is safe to re-run.
- Environment gates prompt for approval again on re-run.

If a downstream infra workflow failed, fix the cause there first; re-running the
orchestrator job dispatches a fresh run of that workflow.
