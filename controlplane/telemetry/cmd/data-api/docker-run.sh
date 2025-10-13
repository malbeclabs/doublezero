#!/usr/bin/env bash
set -euo pipefail
script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
workspace_dir=$(dirname $(dirname $(dirname $(dirname "${script_dir}"))))

cd "${workspace_dir}"

CONTAINER_NAME="${CONTAINER_NAME:-snormore/dz-telemetry-data-api-v3}"

set -x

docker build --platform linux/amd64 -t "${CONTAINER_NAME}" -f controlplane/telemetry/cmd/data-api/Dockerfile .
docker run --rm -it -p 8080:8080 "${CONTAINER_NAME}"
