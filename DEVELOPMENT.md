# DoubleZero Development

DoubleZero is built primarily in **Rust** and **Go**.

## Quickstart

These commands cover the full local development workflow — testing, linting, formatting, and CI validation for both languages.

```bash
# Run Rust and Go tests
make test

# Run Rust and Go linters
make lint

# Format Rust and Go code
make fmt

# Run all CI targets
make ci
```

## Local end-to-end testing

The local devnet and end-to-end test environments both run entirely in Docker containers.

### Getting the Arista cEOS image

The local environment requires the Arista cEOS image to run simulated DZDs (DoubleZero Devices).

### External contributors

1. Register and log in at [arista.com](https://www.arista.com/).
2. Download `cEOS64-lab-4.33.1F.tar` from the [Software Download](https://www.arista.com/en/support/software-download) page.
3. Import and tag it:

    ```bash
    docker import cEOS64-lab-4.33.1F.tar ceos64:4.33.1F
    export ARISTA_CEOS_IMAGE=ceos64:4.33.1F
    ```


### Core contributors (Malbec Labs)

Authenticate with the GitHub container registry:

```bash
echo $GH_TOKEN | docker login ghcr.io -u <github-username> --password-stdin
```

The required image (`ghcr.io/malbeclabs/ceos:4.33.1F`) will be pulled automatically when running the devnet or end-to-end tests.

### Running end-to-end tests

End-to-end tests exercise the full DoubleZero stack — smartcontracts, controller, activator, client, and device agents — all running in isolated Docker containers.

```bash
# Run a specific E2E test directly
cd e2e/
go test -tags e2e -v -run TestE2E_MultiClient

# Or use the helper script
dev/e2e-test.sh TestE2E_MultiClient
```

> ⚠️ Note:
>
>
> E2E tests are resource-intensive. It’s recommended to run them individually or with low parallelism:
>
> ```bash
> go test -tags e2e -v -parallel=1 -timeout=20m
> ```
>
> Running all tests together may require at least 64 GB of memory available to Docker.
>

### Running the local devnet

This starts a full DoubleZero devnet in Docker, including the controller, activator, and DZ ledger with deployed Serviceability and Telemetry programs.

The example below walks through creating a small two-device network with two clients and establishing connectivity between them:

```bash
# Start the local devnet environment
dev/dzctl start -v

# Create a couple devices with a network linking them
dev/dzctl add-device --code dz1 --exchange xams --location ams --cyoa-network-host-id 8 --additional-networks dz1:dz2
dev/dzctl add-device --code dz2 --exchange xlax --location lax --cyoa-network-host-id 16 --additional-networks dz1:dz2

# Register a link onchain
docker exec -it dz-local-manager doublezero link create wan \
  --code dz1:dz2 \
  --contributor co01 \
  --side-a dz1 --side-a-interface Ethernet2 \
  --side-z dz2 --side-z-interface Ethernet2 \
  --bandwidth 10Gbps --mtu 2048 --delay-ms 40 --jitter-ms 3

# Add a couple clients
dev/dzctl add-client --cyoa-network-host-id 100
dev/dzctl add-client --cyoa-network-host-id 110

# Create access passes for the clients
docker exec -it dz-local-manager doublezero access-pass set \
  --accesspass-type prepaid \
  --client-ip 9.169.90.100 \
  --user-payer FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S

docker exec -it dz-local-manager doublezero access-pass set \
  --accesspass-type prepaid \
  --client-ip 9.169.90.110 \
  --user-payer 6gRC1rfTDJP2KzKnBjbcG3LijaVs56fSAsCLyZBU6qa5

# Connect to DoubleZero from the clients
# NOTE: These are example pubkeys in the container names, yours will be different
docker exec -it dz-local-client-FposHWrkvPP3VErBAWCd4ELWGuh2mgx2Wx6cuNEA4X2S \
  doublezero connect ibrl --device dz1

docker exec -it dz-local-client-6gRC1rfTDJP2KzKnBjbcG3LijaVs56fSAsCLyZBU6qa5 \
  doublezero connect ibrl --device dz2

# List running containers
docker ps

# View logs for any component (e.g. the controller)
docker logs -f dz-local-controller

# Open an interactive shell
docker exec -it dz-local-manager bash

# Tear down containers, networks, and volumes
dev/dzctl destroy
```
