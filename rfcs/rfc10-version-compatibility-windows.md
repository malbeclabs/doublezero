# DoubleZero Version Compatibility Windows

## Summary

**Status: Implemented**

This document proposes a windowed version-compatibility model for DoubleZero. The serviceability program will store both:

- `program_version`: the current program version
- `min_compatible_version`: the earliest client version allowed to interoperate

Clients compare their own version against this window:

- `< min_compatible_version` → reject
- `< program_version` but `≥ min_compatible_version` → warn, continue
- `≥ program_version` → fully supported

This provides a predictable, bounded grace period around upgrades.

## Motivation

Today, minor version mismatches cause immediate failures, forcing users to upgrade on the spot. This is disruptive for automated workflows and environments where upgrades cannot be coordinated immediately. A windowed model gives users time to upgrade while still allowing predictable deprecation.

## Terminology

- **Compatibility Window** — Versions `[min_compatible_version, version]`.
- **Grace Period** — The interval during which the previous version remains valid.
- **Deprecated Version** — `< min_compatible_version`, rejected by clients.

## Alternatives Considered

- **Upgrade on every minor release (current)** — Simple, operationally brittle.
- **Epoch-based compatibility** — Feature-gated behaviour by epoch; increases complexity.
- **Version-based window (proposed)** — Predictable, explicit, easy to test.

## Detailed Design

### Onchain State

Global program state includes:

- `program_version`
- `min_compatible_version`

Both are configured directly in the codebase and version-controlled, advancing as part of new releases.

### Client Logic

```python
if client_version < min_compatible_version:
    error
else if client_version < program_version:
    warn
else:
    ok
```

### Rollout Example

- v0.6.12 ships with `min_compatible_version = v0.6.11`
- v0.6.11: allowed, warned
- ≤ v0.6.10: rejected
- v0.6.13 raises `min_compatible_version` to v0.6.12
- v0.6.11 becomes deprecated

### API / Schema Evolution

- During compatibility window:
    - Read both old and new formats
    - Write in the old format until all supported clients can read the new
- When raising `min_compatible_version`:
    - Begin writing only the new format
    - Remove old-format reads in a later release

Invariant: every supported version must read state written by any version in the window.

This is one possible pattern; other approaches are fine as long as the compatibility window is preserved.

### Future Generalization

This proposal focuses on the CLI, but the same model can later support separate compatibility windows for components like the controller, device agents, or the activator. For now, a single unified window keeps things simpler until finer-grained versioning is needed.

## Security Considerations

- **Window size** — A larger compatibility window keeps older code paths active longer, which increases maintenance and complicates auditing.
- **Code cleanup** — Deprecated logic should be removed promptly to avoid carrying unused paths forward.

## Backward Compatibility

The proposal introduces a predictable deprecation boundary while avoiding immediate breakage. Existing deployments require no changes beyond upgrading to the first version that implements this model.

## Open Question

- Should `min_compatible_version` advance only through code releases, or also support an administrative override for rare cases where it must change between releases?
