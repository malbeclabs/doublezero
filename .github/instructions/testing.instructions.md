---
applyTo: "**/*_test.go,**/tests/**,e2e/**,smartcontract/**/src/**/*.rs,crates/**/src/**/*.rs,client/doublezero/src/**/*.rs"
description: "Review rules for tests and test coverage"
---

# Testing

These rules apply to test code only — files under `tests/`, `*_test.go`, and `#[cfg(test)]` modules
inside Rust sources. If the diff contains no test code, this file has nothing to say; do not
manufacture a finding from it.

## Assertions

- Assert the specific error, never `is_err()`, a bare `assert!(result.is_err())`, or
  `error_string.contains("Custom(65)")`. For program tests that means the exact
  `InstructionError(index, InstructionError::Custom(code))`, with `code` derived from the error enum
  rather than inlined as a number, so renumbering the enum cannot silently rot the test.
- When a type derives `PartialEq`, compare the whole struct with `assert_eq!` instead of checking
  fields one at a time. A field added later is then covered automatically instead of passing
  silently.
- Pin the exact shape, not its size: the full `Vec<AccountMeta>` rather than its length, the exact
  rendered CLI output rather than `contains("feed01")`, the exact JSON object rather than a
  presence check. `mockall`'s `predicate::always()` is not an assertion — build the expected value
  from the test's own inputs and use `predicate::eq`.
- After a rejection test, re-read the state and assert it is unchanged. A transaction that fails for
  the right reason can still have written something.

## Coverage

- Every guard, rejection branch, and error path the diff adds needs a test that reaches it. A new
  error variant with no negative test is a finding.
- Cover both sides of an optional or variable-length shape: with and without the optional trailing
  account, each independent conditional path, the empty and populated collection. A function with
  three independent conditionals needs a case that isolates each one.
- Wiring needs coverage, not just the pure helper it calls. A well-tested parser behind an untested
  call site is an untested feature.

## The test must be able to fail

- If a test's assertions do not depend on any line the diff changes, say so and ask the author to
  confirm it fails on the base branch. Do not assert that it passes there — you cannot run it. A PR
  description that calls such a test a regression test is the thing to flag.
- Flag a test that restates the expression under test. Re-deriving the expected value with the same
  helpers the implementation uses proves nothing about drift.
- Flag a test whose measurement window closes before the event it claims to cover can fire, or whose
  setup makes the condition unreachable. If nothing drains the queue, the test cannot say anything
  about rate.
- If two guards return the same error code, a test asserting that code cannot tell them apart.
  Require test input that isolates the branch, or distinct error variants.
- Do not test framework or SDK behavior (that `find_program_address` is deterministic, that serde
  round-trips). Test this repository's logic.
