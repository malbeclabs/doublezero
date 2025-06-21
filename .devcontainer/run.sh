#!/bin/bash
set -eou pipefail
script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
set -a; source "${script_dir}/.env.local"; set +a

workspace_dir="$(dirname "${script_dir}")"
cd "${workspace_dir}"

# Build the workspace image.
"${script_dir}/build.sh"

# Start and attach to the workspace container.
echo "==> Starting workspace container..."
if [ -n "${CMD:-}" ]; then
    command=(bash -c "${CMD}")
else
    command=(bash)
fi
(
    set -x
    docker run -it --rm \
        --name "${DZ_DEV_WORKSPACE_NAME}" \
        --volume "${workspace_dir}:/workspace" \
        --workdir "/workspace" \
        --volume /var/run/docker.sock:/var/run/docker.sock \
        --volume "${SSH_AUTH_SOCK}:/ssh-agent" \
        --env "SSH_AUTH_SOCK=/ssh-agent" \
        --cap-add=NET_ADMIN \
        --cap-add=NET_RAW \
        "${DZ_DEV_WORKSPACE_IMAGE}" \
        "${command[@]}"
)
