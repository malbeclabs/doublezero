---
applyTo: "**/CHANGELOG.md"
description: "Review rules for CHANGELOG entries"
---

# CHANGELOG

- A change to who may call an instruction, or to what an existing caller must do, belongs under
  `### Breaking` with the deploy note, not `### Changes`. An operator reading `### Changes` has no
  reason to check their allowlist membership before upgrading.
- The entry must describe what the merged code does, checked against the final state of the diff
  rather than the PR's first commit. Wording written before a design pivot is a defect, and a
  change that also alters behavior must not be described as a pure refactor — if a bug fix ships to
  consumers through it, name it.
- If the change only works when something else happens first — a database grant applied before the
  deploy, a feature flag left off until a later release, a version bump ordered against another
  component — the entry must say so. An operator who reads the entry and deploys must not be able
  to break the system by following it.
- Classify against semantic versioning (RFC-1): a patch is a backwards-compatible fix, a minor is
  backwards-compatible new functionality, and anything that breaks an API or a wire format is
  major. Flag an entry that files a contract break under a fix or a feature — compatibility is
  guaranteed only for one subsequent minor release, so a misclassified break auto-upgrades users
  into it.
- Reference the PR number as `(#NNNN)`, not the branch name or the infra ticket it came from.
