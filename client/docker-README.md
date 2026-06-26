# DoubleZero client Docker image

A thin Ubuntu image with the DoubleZero client (`doublezero` CLI + `doublezerod`
daemon) installed from the official Cloudsmith apt repo. Published as
`ghcr.io/malbeclabs/doublezero`.

## Build

```bash
# latest published client
docker build -t doublezero client/

# pin a specific released version
docker build --build-arg DZ_VERSION=1.2.3 -t doublezero:1.2.3 client/
```

## Run

The client manages GRE tunnels and routes, so the container needs network
capabilities and a tun device:

```bash
docker run --rm \
  --network host \
  --cap-add NET_ADMIN --cap-add NET_RAW \
  --device /dev/net/tun \
  ghcr.io/malbeclabs/doublezero status
```

The entrypoint starts `doublezerod`, waits for its socket, then:

- **with arguments** — runs `doublezero <args>` (e.g. `status`, `latency`,
  `connect ...`) and exits.
- **without arguments** — prints `doublezero status` and stays running on the
  daemon, so you can `docker exec -it <container> doublezero <args>`.

### Configuration

| Env var   | Default                              | Purpose                                  |
| --------- | ------------------------------------ | ---------------------------------------- |
| `DZ_ENV`  | `mainnet-beta`                       | DoubleZero environment for the daemon and CLI |
| `DZ_SOCK` | `/run/doublezerod/doublezerod.sock`  | doublezerod Unix socket path             |

## Not yet wired up

- **Publishing**: a `release.docker.client.yml` workflow (modeled on
  `release.docker.core.yml`) will build and push the image after `client/v*.*.*`
  releases land in Cloudsmith.
