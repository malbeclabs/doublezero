# IP ownership verification in the local devnet

RFC-27 ([`rfcs/rfc27-ip-verification.md`](../../rfcs/rfc27-ip-verification.md)) has `connect`
attach a proof, signed by a DoubleZero-operated verifier, that the caller can originate traffic
from the `client_ip` it is binding. `dev/dzctl` runs that verifier so the local flow matches
production.

## What comes up

`dzctl start` brings up a `dz-local-ip-verifier` container (image `dz-local/ip-verifier:dev`)
alongside the rest of the stack:

- **Keypair**: generated per deploy into `dev/.deploy/dz-local/ip-verifier-keypair.json`. Devnet
  only — nothing is checked in.
- **Onchain authority**: the keypair's pubkey is written to
  `GlobalState.ip_verifier_authority_pk` before the container starts. The service reads the
  authority from the ledger at startup and exits if it does not name its own key, so the order
  matters.
- **Networks**: the default network (to reach the ledger) *and* the CYOA network, on host ID 250.
- **Client wiring**: every client container gets `DZ_IP_VERIFIER_URL` pointing at the verifier's
  **CYOA** address.

That last pair is the point. The verifier signs the source address it observes the request arrive
from, and `connect` refuses a proof for any address other than the one it is provisioning. A local
client provisions its CYOA address, so the request has to reach the verifier over the CYOA network
for the two to agree — reached over the default network instead, the observed address would be the
client's default-network address and every connect would hard-fail on the mismatch. This is the
same class of problem as the proxy handling in production, where the address the service sees is
the proxy's unless it is configured to read a forwarded one.

The CYOA subnet is allocated from `9.128.0.0/9`, which is globally routable, so the verifier's
`not_globally_routable` refusal (which an RFC-1918 source would hit) does not fire.

## Enforcement is off by default

The `require-ip-ownership-proof` feature flag is **clear** in the local `GlobalState`. A proof is
obtained and attached, but the program accepts a create without one — so a stack where the
verifier is down, or a client that cannot reach it, still connects. That mirrors an environment
whose rollout has not flipped the flag yet.

To exercise the enforcement path, turn it on:

```bash
docker exec dz-local-manager \
  doublezero global-config feature-flags set --enable require-ip-ownership-proof
```

and off again:

```bash
docker exec dz-local-manager \
  doublezero global-config feature-flags set --disable require-ip-ownership-proof
```

From a Go e2e test, `devnet.SetIPOwnershipProofFeatureFlag(ctx, true)` does the same thing.

## Poking at it

```bash
# Health: 200 once the cached ledger epoch is fresh and the ledger names this key.
docker exec dz-local-ip-verifier curl -sS localhost:8080/health

# What the ledger thinks the authority is.
docker exec dz-local-manager doublezero global-config authority get

# A proof, as a client would ask for it.
docker exec dz-local-client-<pubkey> \
  curl -sS -X POST "$DZ_IP_VERIFIER_URL/v1/proof" \
  -H 'content-type: application/json' \
  -d '{"payer":"<pubkey>","user_type":0}'
```

The rate limit is raised well above the production default in the devnet (burst 1000, 6000/min):
a devnet has one source address per client and a test can reconnect in a tight loop, which the
production values would turn into `rate_limited` refusals unrelated to what is being tested.
