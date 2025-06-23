#!/bin/bash
set -eou pipefail
script_dir="$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"
set -a; source "${script_dir}/../.env.local"; set +a

workspace_dir="$(dirname "$(dirname "${script_dir}")")"
echo "==> Workspace directory: ${workspace_dir}"

relative_dockerfiles_dir="e2e/docker"
dockerfiles_dir="${workspace_dir}/${relative_dockerfiles_dir}"
echo "==> Dockerfiles directory: ${dockerfiles_dir}"

# Show commands as they are executed.
if [ -n "${DEBUG:-}" ]; then
    set -x
fi

# Get the solana tools version.
solana_version="$(docker run --rm "${DZ_SOLANA_IMAGE}" solana --version | awk '{print $2}')"

# Build the base image.
docker build -t "${DZ_BASE_IMAGE}" -f "${dockerfiles_dir}/base.dockerfile" --build-arg SOLANA_IMAGE="${DZ_SOLANA_IMAGE}" --build-arg SOLANA_VERSION="${solana_version}" "${workspace_dir}"

# Build the control plane images.
docker build -t "${DZ_LEDGER_IMAGE}" -f "${dockerfiles_dir}/ledger/Dockerfile" --build-arg BASE_IMAGE="${DZ_BASE_IMAGE}" --build-arg DOCKERFILE_DIR="${relative_dockerfiles_dir}/ledger" "${workspace_dir}"
docker build -t "${DZ_CONTROLLER_IMAGE}" -f "${dockerfiles_dir}/controller/Dockerfile" --build-arg BASE_IMAGE="${DZ_BASE_IMAGE}" --build-arg DOCKERFILE_DIR="${relative_dockerfiles_dir}/controller" "${workspace_dir}"
docker build -t "${DZ_ACTIVATOR_IMAGE}" -f "${dockerfiles_dir}/activator/Dockerfile" --build-arg BASE_IMAGE="${DZ_BASE_IMAGE}" --build-arg DOCKERFILE_DIR="${relative_dockerfiles_dir}/activator" "${workspace_dir}"
docker build -t "${DZ_MANAGER_IMAGE}" -f "${dockerfiles_dir}/manager/Dockerfile" --build-arg BASE_IMAGE="${DZ_BASE_IMAGE}" --build-arg DOCKERFILE_DIR="${relative_dockerfiles_dir}/manager" "${workspace_dir}"

# Build the data plane images.
docker build -t "${DZ_DEVICE_IMAGE}" -f "${dockerfiles_dir}/device/Dockerfile" --build-arg BASE_IMAGE="${DZ_BASE_IMAGE}" --build-arg DOCKERFILE_DIR="${relative_dockerfiles_dir}/device" "${workspace_dir}"
docker build -t "${DZ_CLIENT_IMAGE}" -f "${dockerfiles_dir}/client/Dockerfile" --build-arg BASE_IMAGE="${DZ_BASE_IMAGE}" --build-arg DOCKERFILE_DIR="${relative_dockerfiles_dir}/client" "${workspace_dir}"

echo "==> Finished building images."
