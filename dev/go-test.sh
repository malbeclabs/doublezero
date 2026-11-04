#!/usr/bin/env bash
set -euo pipefail

script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
workspace_dir=$(dirname "${script_dir}")

image="dz-go-test"

build_log="$(mktemp)"
if ! docker build --progress=plain -t "${image}" -f "${script_dir}/Dockerfile.go-test" "${script_dir}" >"${build_log}" 2>&1; then
    cat "${build_log}" >&2
    rm -f "${build_log}"
    exit 1
fi
rm -f "${build_log}"

exec docker run --rm \
    -v "${workspace_dir}:/workspace" \
    -v /var/run/docker.sock:/var/run/docker.sock \
    -w /workspace \
    --cap-add NET_ADMIN \
    --cap-add NET_RAW \
    "${image}" \
    make go-test-docker "$@"
