---
applyTo: "e2e/**"
description: "Review rules for end-to-end and QA tests"
---

# End-to-end and QA tests

These tests run on a schedule against live networks and are an alerting surface. A test that fails
to fail is worse than no test.

- A skip must mean "the feature is not configured on this network". If the feature is configured but
  its resources are unreachable or missing, fail loudly and name them. A helper that returns
  `(nil, nil)` for both cases turns an outage of the feature under test into a silent scheduled
  skip. The same applies to a classifier that decides a failure is benign: on anomalous input —
  an unknown enum value, an out-of-range reading — it must refuse to classify rather than default
  to benign.
- `t.Run` returns `true` for a skipped subtest. After a subtest that can skip, guard its result at
  the parent (`if device == nil { t.Skip(...) }`) or the next subtest panics dereferencing it.
- One read of live chain state is not proof. A stale or lagging endpoint serves a plausible
  not-found for a young account, and a health probe that checks slot height can pass an endpoint
  that is stale for one specific method. Require a second read after a failover, or a state field
  that staleness cannot explain — and encode the SDK's documented contract, not the one field that
  happened to work in a manual run.
- On an `Eventually` or poll timeout, log the last observed state — the subscribed group pubkeys,
  the seat state, the booleans being compared — so on-call can tell the failure modes apart without
  a rerun. Generic diagnostic dumps do not cover onchain state.
- Do not copy a long test flow. Extract the shared skeleton into a helper parameterized on what
  differs; a copied flow drifts silently the next time the original is fixed.
