#!/usr/bin/env bash
set -euo pipefail

script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
workspace_dir=$(dirname "${script_dir}")

test=${1:-}

if [ -n "${test}" ]; then
    make -C "${workspace_dir}/e2e" test RUN="${test}"
else
    make -C "${workspace_dir}/e2e" test
fi
