#!/usr/bin/env bash
set -euo pipefail

script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
workspace_dir=$(dirname "${script_dir}")

image="dz-go-test"

docker build -q -t "${image}" -f "${script_dir}/Dockerfile.go-test" "${script_dir}" >&2

exec docker run --rm \
    -v "${workspace_dir}:/workspace" \
    -w /workspace \
    "${image}" \
    make go-lint-native "$@"
