# Building the allowlist sync image

`doublezero-telemetry-allowlist-sync` runs on dz-k8s-testnet and dz-k8s-mainnet-beta from its own image, `malbeclabs/doublezero-telemetry-allowlist-sync` in each account's ECR. infra pins it by digest in `k8s/dz/telemetry/dz-k8s-testnet/kustomization.yaml` and `k8s/dz/telemetry/dz-k8s-mainnet-beta/kustomization.yaml`. The image is not part of a doublezero release, and no workflow builds it.

## Build

Build the approved merge commit on `main` from a fresh clone, so nothing uncommitted ends up in the image. Set `SHA` to that commit. The tag is `sha-` and its first 8 characters; ECR tags are immutable, so one tag always means one image. One build pushes to both accounts' ECRs, `967501763874.dkr.ecr.us-east-1.amazonaws.com` (mainnet-beta) and `764988199339.dkr.ecr.us-east-1.amazonaws.com` (testnet), so both hold the same manifest list and one digest. Log in to both first. With `BUILDX_GIT_LABELS=1` the image is labeled with the commit, and `BUILDX_GIT_CHECK_DIRTY=1` labels a dirty tree `<sha>-dirty`. The block runs in a subshell, so it can be pasted into a terminal: `set -e` stops the subshell at the first failure, so a failed clone or test never builds or pushes, and neither that nor the `cd` touches the terminal's own shell.

```
(
set -euo pipefail
: "${SHA:?set SHA to the merge commit}"
TAG="sha-${SHA:0:8}"
MN=967501763874.dkr.ecr.us-east-1.amazonaws.com/malbeclabs/doublezero-telemetry-allowlist-sync
TN=764988199339.dkr.ecr.us-east-1.amazonaws.com/malbeclabs/doublezero-telemetry-allowlist-sync
D=$(mktemp -d)
git clone -q git@github.com:malbeclabs/doublezero.git "$D"
cd "$D"
git checkout -q "$SHA"
go test -race -count=1 ./telemetry/allowlist-sync/... ./smartcontract/sdk/go/serviceability/...
BUILDX_GIT_LABELS=1 BUILDX_GIT_CHECK_DIRTY=1 docker buildx build --platform linux/amd64,linux/arm64 \
  -f telemetry/allowlist-sync/Dockerfile \
  --build-arg BUILD_VERSION="$TAG" --build-arg BUILD_COMMIT="$SHA" \
  --build-arg BUILD_DATE="$(git show -s --format=%cI "$SHA")" \
  -t "$MN:$TAG" -t "$TN:$TAG" --push .
MN_DIGEST=$(docker buildx imagetools inspect "$MN:$TAG" --format '{{printf "%s" .Manifest.Digest}}')
TN_DIGEST=$(docker buildx imagetools inspect "$TN:$TAG" --format '{{printf "%s" .Manifest.Digest}}')
[ "$MN_DIGEST" = "$TN_DIGEST" ] || { echo "digests differ: $MN_DIGEST $TN_DIGEST" >&2; false; }
echo "$MN_DIGEST"
)
```

The last line prints the digest, bare, as the overlays take it. If the two registries hold different digests, the line before it fails and `set -e` stops there. Pin that one digest in both overlays. Two separate builds would give two digests, and either overlay pinned to the other's would fail its image pull.

`BUILD_DATE` is the commit's date, not the build's, so the version the binary reports (`doublezero_telemetry_allowlist_sync_build_info`) is the same however often the commit is built.
