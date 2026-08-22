# doublezero-ip-verifier

The RFC-27 IP ownership verification service. See
[`rfcs/rfc27-ip-verification.md`](../../rfcs/rfc27-ip-verification.md).

A stateless HTTP signer. It observes the source address of a request and returns an
`IpOwnershipProof` — `{ version, payer, client_ip, epoch, user_type, signature }` — signed by the
verifier keypair. The serviceability program validates that proof before it will bind a `client_ip`
to a user, so this service is the only party that can attest an address. Its answer to "which
address did I actually see?" is the whole security property.

## API

```
POST /v1/proof
{ "payer": "<base58 pubkey>", "user_type": <u8> }

200 { "version": 1, "payer": "...", "client_ip": "a.b.c.d", "epoch": 1234,
      "user_type": 3, "signature": "<base58>" }
```

`client_ip` is never read from the body; sending one is a `400`, not a field that gets ignored. The
response does not carry the verifier public key: a client takes that from `GlobalState`, the same
place the program reads it.

Errors are `{ "error": "<reason>", "message": "..." }`, where `reason` is one of
`invalid_request`, `rate_limited`, `ipv6_unsupported`, `not_globally_routable`,
`invalid_forwarded_header`, `client_ip_unresolved`, `epoch_unavailable`, or
`verifier_key_mismatch` — the same strings that label the
`doublezero_ip_verifier_proofs_refused_total` metric.

`GET /health` returns `200` while the cached ledger epoch is usable and this service's key is the
authority the program accepts, and `503` otherwise, so an instance that cannot issue a usable proof
drops out of rotation. The body carries `epoch` and `verifier_key`
(`matches` / `mismatch` / `unknown`). Prometheus metrics are served on the separate
`--metrics-addr` listener, which is not meant to be publicly reachable.

## Running

```bash
cargo run -p doublezero-ip-verifier -- \
  --env testnet \
  --keypair /etc/doublezero/ip-verifier.json \
  --listen-addr 0.0.0.0:8080
```

Every flag has a `DZ_IP_VERIFIER_`-prefixed environment variable; see `--help`.

The keypair's public key must equal `GlobalState.ip_verifier_authority_pk`, or the program rejects
every proof this service issues. That is checked rather than assumed: the authority is read from the
ledger at startup, where a mismatch is a startup error, and re-read every
`--authority-refresh-secs`, where a rotation this instance was not redeployed for turns `/health`
red and makes the proof endpoint refuse. An unreadable `GlobalState` is only a warning — an RPC
problem already shows up as a stale epoch, and failing to start on one would be its own outage.

## Proxies

`--trusted-proxy <CIDR>` (repeatable, or comma-separated) is the only thing that makes forwarded
headers count. `--forwarded-header` names which header is read: `x-forwarded-for` (default) or
`forwarded`.

- **No trusted proxies** — forwarded headers are ignored entirely and the connection peer address is
  signed. This is correct when clients reach the service directly.
- **Peer inside a trusted CIDR** — the configured header's chain is walked from the right, and the
  first hop that is not itself a trusted proxy is the client. Hops a client prepended sit to the left
  of what our nearest proxy observed, so they are ignored, and are never even parsed — an
  `X-Forwarded-For: unknown` from the client does not break a request whose real hop is valid.
- **Nothing resolvable** — no forwarded header from a trusted proxy, a chain of only trusted hops, or
  junk where the client hop should be: nothing is signed. Signing the proxy's own address for
  whoever is behind it is the failure this service exists to avoid, so it fails closed.

Set `--forwarded-header` to what the proxy in front of this service actually writes. Only that
header is read, and the other is ignored even when present: a proxy configured for RFC 7239
`Forwarded` that does not also set or strip `X-Forwarded-For` passes the client's own
`X-Forwarded-For` through untouched, so a service that preferred whichever header was present could
be handed a client-chosen address.

Get `--trusted-proxy` wrong in the other direction — trusting a CIDR that is not a proxy — and any
client in that range can name its own address. It should list only proxies DoubleZero operates.

## Limits

IPv4 only: the v1 proof layout carries a 4-byte address, so an IPv6 source is refused rather than
mapped onto something IPv4-shaped. IPv4-mapped addresses (`::ffff:a.b.c.d`, what a dual-stack
`[::]` listener reports for every IPv4 client) are collapsed to their IPv4 form first, for both the
peer address and forwarded hops. Addresses the program's own `is_global` predicate rejects are
refused too, so the service never hands out a proof that could only fail onchain.
