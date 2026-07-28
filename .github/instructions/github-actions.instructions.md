---
applyTo: ".github/workflows/**,.github/actions/**,scripts/release/**"
description: "Review rules for release workflows, composite actions, and release scripts"
---

# GitHub Actions and release scripts

These workflows push mutable tags, packages, and onchain program artifacts. Treat a silent no-op as
a defect even when nothing is currently broken.

- Any job that pushes a mutable tag (`:latest`, `:mainnet-beta`) or otherwise races another trigger
  needs a job-level `concurrency` group with `cancel-in-progress: false`. Cancelling mid-push leaves
  a partially updated multi-tag set, and a "has it changed" gate read at job start is an
  optimization, not a lock.
- A best-effort notification step that runs after an irreversible push needs
  `continue-on-error: true`. Otherwise a token blip reddens a job whose artifact published fine, and
  the re-run force-republishes and fires a second dispatch.
- Prefer `if: ${{ !cancelled() }}` over `if: always()` for guard jobs. `always()` makes every
  deliberately cancelled run end red, and can fire the failure notification.
- `failure()` evaluates over the whole transitive `needs` chain, so adding a job to a guard's
  `needs` can defeat a deliberate exclusion documented elsewhere and page for a release that
  succeeded. When a diff changes a `needs` list, ask whether the comment describing the exclusion is
  still true rather than asserting it is not.
- When several jobs must build and tag the same content, resolve one commit SHA and thread it
  through every checkout and every dispatched workflow. A floating `main` checkout lets tags,
  packages, and program artifacts diverge if anything lands mid-run, and a `skip_existing` guard
  will silently accept a tag left from an earlier partial run against a different commit unless it
  logs what that tag points at.
- Version comparisons in shell need anchored or literal matching. An unescaped `.` in a `grep -E`
  pattern makes `0.27.1` match `0.27.10`, so a release verifies itself against the wrong version;
  escape interpolated variables before using them in a pattern.
- Scope minted app tokens with the `permission-*` inputs. `owner`/`repositories` alone mints a token
  carrying every permission the App holds on the repository, which does not make a "least-privilege"
  comment true.
- A cross-repository `createDispatchEvent` returns 204 whether or not a receiver is listening, so a
  drifted `event_type` or payload shape fails invisibly rather than erroring.
- Flag a new job that duplicates an existing one. Any job that stays needs `timeout-minutes`, or a
  hung step runs to the six-hour default.
