#!/bin/bash
set -eou pipefail
script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
set -a; source "${script_dir}/.env.local"; set +a

workspace_dir="$(dirname "${script_dir}")"
cd "${workspace_dir}"

if [ -z "${BUILDX:-}" ]; then
    echo "==> Building development container..."
    (
        set -x
        docker build -t "${DZ_DEV_WORKSPACE_IMAGE}" -f .devcontainer/Dockerfile .
    )
else
    push_flag=""
    if [ -n "${PUSH:-}" ]; then
        push_flag="--push"
    fi
    echo "==> Building development container for multiple platforms..."
    (
        set -x
        docker buildx build --platform linux/amd64,linux/arm64 -f .devcontainer/Dockerfile -t ${DZ_DEV_WORKSPACE_IMAGE} ${push_flag} .
    )
fi
