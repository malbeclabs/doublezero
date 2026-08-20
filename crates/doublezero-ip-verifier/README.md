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
`client_ip_unresolved`, or `epoch_unavailable` — the same strings that label the
`doublezero_ip_verifier_proofs_refused_total` metric.

`GET /health` returns `200` while the cached ledger epoch is usable and `503` otherwise, so an
instance that cannot issue a usable proof drops out of rotation. Prometheus metrics are served on
the separate `--metrics-addr` listener, which is not meant to be publicly reachable.

## Running

```bash
cargo run -p doublezero-ip-verifier -- \
  --env testnet \
  --keypair /etc/doublezero/ip-verifier.json \
  --listen-addr 0.0.0.0:8080
```

Every flag has a `DZ_IP_VERIFIER_`-prefixed environment variable; see `--help`. The keypair's public
key must equal `GlobalState.ip_verifier_authority_pk`, or the program rejects every proof this
service issues.

## Proxies

`--trusted-proxy <CIDR>` (repeatable, or comma-separated) is the only thing that makes forwarded
headers count.

- **No trusted proxies** — forwarded headers are ignored entirely and the connection peer address is
  signed. This is correct when clients reach the service directly.
- **Peer inside a trusted CIDR** — the forwarded chain (`X-Forwarded-For`, else RFC 7239
  `Forwarded`) is walked from the right, and the first hop that is not itself a trusted proxy is the
  client. Hops a client prepended sit to the left of what our nearest proxy observed, so they are
  ignored.
- **Nothing resolvable** — no forwarded header from a trusted proxy, a chain of only trusted hops, or
  an entry that is not an address: nothing is signed. Signing the proxy's own address for whoever is
  behind it is the failure this service exists to avoid, so it fails closed.

Get this wrong in the other direction — trusting a CIDR that is not a proxy — and any client in that
range can name its own address. `--trusted-proxy` should list only proxies DoubleZero operates.

## Limits

IPv4 only: the v1 proof layout carries a 4-byte address, so an IPv6 source is refused rather than
mapped onto something IPv4-shaped. Addresses the program's own `is_global` predicate rejects are
refused too, so the service never hands out a proof that could only fail onchain.
