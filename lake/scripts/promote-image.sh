#!/bin/bash
set -euo pipefail

# Promote a lake image from one tag to another (e.g., staging → prod)
# Usage: ./promote-image.sh [source_tag] [target_tag]

SOURCE_TAG="${1:-snor-lake-v3}"
TARGET_TAG="${2:-prod}"
IMAGE="ghcr.io/malbeclabs/doublezero-lake"

echo "Promoting ${IMAGE}:${SOURCE_TAG} → ${IMAGE}:${TARGET_TAG}"

# Ensure we're logged in to GHCR
if ! docker pull "${IMAGE}:${SOURCE_TAG}" 2>/dev/null; then
    echo "Login to GHCR required..."
    echo "$(gh auth token)" | docker login ghcr.io -u malbeclabs --password-stdin
    docker pull "${IMAGE}:${SOURCE_TAG}"
fi

docker tag "${IMAGE}:${SOURCE_TAG}" "${IMAGE}:${TARGET_TAG}"
docker push "${IMAGE}:${TARGET_TAG}"

echo "✅ Promoted successfully"
echo ""
echo "ArgoCD will pick up the new digest automatically."
echo "To force immediate sync: kubectl -n argocd patch app lake-prod --type merge -p '{\"metadata\":{\"annotations\":{\"argocd.argoproj.io/refresh\":\"hard\"}}}'"
