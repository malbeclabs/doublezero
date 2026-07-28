---
applyTo: "sdk/**,smartcontract/sdk/go/**"
description: "Review rules for the Go/Python/TypeScript SDKs and their golden fixtures"
---

# SDKs and golden fixtures

The SDKs decode accounts serialized by the Rust programs. Golden fixtures generated from Rust are
the only thing that catches cross-language wire drift, so their strictness is the coverage.

- Golden `.bin`/`.json` fixtures are committed and the fixture tests read them from disk. A test
  that regenerates its own input asserts nothing, and a test reading a file the repository does not
  contain fails on every fresh checkout. CI regenerates the goldens only to `git diff` them for
  drift (`make check-fixtures`) — do not add a second job that does the same thing.
- A new or changed account field must be asserted in every SDK that ships a decoder for it — Go,
  Python, and TypeScript — against the shared fixture. A field-order regression that only one
  language covers still passes CI.
- Borsh-incremental appended fields need a `*_legacy` fixture (serialized, then truncated at the
  appended field) asserting the language's default, and ideally a `*_future_version` fixture with
  unknown trailing bytes. Without them the fallback decode path has zero cross-language coverage.
- Fixture tests must be bidirectional. Re-serialize the decoded struct and compare bytes against the
  fixture, and assert the decoder drained the buffer — decode-only tests let the encode direction
  drift silently.
- Assertions must fail when a field is added. Compare field counts or total byte length rather than
  only the fields the test happens to name; a one-directional check that errors only on a missing
  JSON key leaves new struct fields unasserted.
- Build fixture inputs from distinct nonzero values per field. An all-default payload pins neither
  field order, endianness, signedness, nor `Option` encoding — a `None` byte is indistinguishable
  from a zeroed field, and two same-typed fields can be swapped invisibly.
- Distinguish an empty collection from an absent field when decoding. Collapsing a present
  length-zero vector to `nil` loses information the caller may need.
