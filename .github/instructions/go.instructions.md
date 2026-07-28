---
applyTo: "client/**/*.go,controlplane/**/*.go,telemetry/**/*.go,api/**/*.go,tools/**/*.go,config/**/*.go"
description: "Review rules for the Go daemon, controller, telemetry, tooling, and shared env config"
---

# Go

## Lifecycle and concurrency

- A component that can be started more than once per process lifetime must restart cleanly. Signal
  shutdown with `done <- struct{}{}` and reset the `WaitGroup` in `Start`, rather than
  `close(done)` behind a `sync.Once` — a permanently closed channel makes every later `Start`
  return immediately, silently disabling the feature until the process restarts. Check how the
  sibling senders in the same package do it.
- An unbuffered channel written by a caller on the reconcile path must be drained by every `select`
  in the receiving goroutine, including jitter and backoff waits, or it must be buffered. Otherwise
  a routine update blocks reconciliation for the length of the wait.
- Verify the "only one goroutine touches this" assumption rather than inheriting it — count the
  places the owning struct is constructed and `Run` is called. Shared counters need a mutex or an
  atomic.
- Unwind already-started senders, sockets, and goroutines when a later step of setup fails.
  Discarding a partially provisioned service leaks a goroutine and a raw socket on every retry.

## Correctness

- Adding a field to a struct means adding it to that struct's `Equal` and `Diff`. A caller that
  gates on `Equal` before consulting anything else makes the new comparison dead code, so the
  feature never takes effect.
- Do not discard errors. A swallowed check error makes a persistently failing read invisible in the
  run log; a discarded `strconv.Parse*` error turns malformed input into a silent zero that can
  match the decoded value and pass the assertion.
- Treat enum, phase, and status values as an allowlist. An unknown value must return an error, not
  fall into the safe-looking branch of a denylist.
- Error-string matching against JSON-RPC decode failures must be written for the decoder actually in
  use: solana-go decodes RPC bodies with `goccy/go-json`, not `encoding/json`, so fixtures built
  with the standard library exercise a decoder the client never runs. (Account borsh decoding goes
  through a different path and is unaffected.)

## Operational behavior

- Every network call inside a polling loop needs its own `context.WithTimeout`. An endpoint that
  accepts connections and never responds otherwise blocks a tick for minutes and makes the ticker
  drop ticks. Escalation policies that count failures rather than elapsed time mis-calibrate when
  each failure is slow.
- Cap externally-supplied strings before buffering them or writing them to `LowCardinality` columns;
  a hostile or buggy caller otherwise bloats memory and degrades dictionary efficiency.
