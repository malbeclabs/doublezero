#!/usr/bin/env bash
set -euo pipefail

script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
workspace_dir=$(dirname "${script_dir}")

test=${1:-}

cd "${workspace_dir}/e2e"

if [ -n "${test}" ]; then
    go test -v -tags e2e -run="${test}" -timeout 20m
else
    make test verbose
fi
