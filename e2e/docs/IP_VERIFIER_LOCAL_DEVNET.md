# IP ownership verification in the local devnet

RFC-27 ([`rfcs/rfc27-ip-verification.md`](../../rfcs/rfc27-ip-verification.md)) has `connect`
attach a proof, signed by a DoubleZero-operated verifier, that the caller can originate traffic
from the `client_ip` it is binding. Every devnet runs that verifier — `dev/dzctl` and the Go e2e
suite alike — so the local flow matches production.

## What comes up

The verifier is on by default: `IPVerifierSpec.Disabled` is the opt-out, so a devnet that says
nothing about it gets one. `dzctl start` brings up a `dz-local-ip-verifier` container (image
`dz-local/ip-verifier:dev`) alongside the rest of the stack; an e2e test gets the same container
named for its own deploy ID:

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

## From a Go e2e test

Because the verifier is on by default, an ordinary `connect` in any e2e test already obtains and
attaches a real proof. Two knobs cover the cases that need something else:

- `ClientSpec.NoIPVerifier` leaves `DZ_IP_VERIFIER_URL` unset for one client, so its `connect`
  obtains no proof at all — the path an environment takes before its verifier exists.
- `IPVerifierSpec.AuthorityRefreshSecs` pins how often the service re-reads the onchain authority.
  Set it long and rotate the authority with `devnet.SetIPVerifierAuthority` and the service keeps
  signing with a key `GlobalState` no longer names, which is how a test produces a proof that gets
  refused.

`e2e/ip_ownership_proof_test.go` uses all three paths.

## Poking at it

```bash
# Health: 200 once the cached ledger epoch is fresh and the ledger names this key.
docker exec dz-local-ip-verifier curl -sS localhost:8080/health

# What the ledger thinks the authority is.
docker exec dz-local-manager doublezero global-config authority get

# A proof, as a client would ask for it. Run through a shell in the container: DZ_IP_VERIFIER_URL
# is set in the client's environment, and `docker exec curl` would have the host shell expand it.
docker exec dz-local-client-<pubkey> bash -c \
  'curl -sS -X POST "$DZ_IP_VERIFIER_URL/v1/proof" \
   -H "content-type: application/json" \
   -d "{\"payer\":\"<pubkey>\",\"user_type\":0}"'
```

The rate limit is raised well above the production default in the devnet (burst 1000, 6000/min):
a devnet has one source address per client and a test can reconnect in a tight loop, which the
production values would turn into `rate_limited` refusals unrelated to what is being tested.
