#!/bin/bash
set -euo pipefail

# Deploy lake-api to Kubernetes
#
# Usage:
#   ./scripts/deploy-app.sh [--dry-run] [--skip-assets] [--skip-build] [--skip-push] [--skip-deploy]
#
# Options:
#   --dry-run, -n     Show what would be done without actually doing it
#   --skip-assets     Skip uploading assets to S3
#   --skip-build      Skip building Docker image (use existing)
#   --skip-push       Skip pushing Docker image
#   --skip-deploy     Skip Kubernetes deployment/rollout
#
# Environment:
#   ASSET_BUCKET - S3 bucket for web assets (required unless --skip-assets)
#   ASSET_BUCKET_PREFIX - Prefix in bucket (default: "assets")
#   GOOGLE_CLIENT_ID - Google OAuth client ID (or VITE_GOOGLE_CLIENT_ID)
#   DOCKER_REGISTRY - Docker registry (default: "snormore")
#   DOCKER_IMAGE - Image name (default: "doublezero-lake")
#   K8S_NAMESPACE - Kubernetes namespace (default: "doublezero-data")
#   K8S_DEPLOYMENT - Deployment name (default: "lake-api")
#   K8S_CONTAINER - Container name (default: "lake-api")

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
REPO_ROOT="$(dirname "$ROOT_DIR")"

DRY_RUN=""
SKIP_ASSETS=""
SKIP_BUILD=""
SKIP_PUSH=""
SKIP_DEPLOY=""

while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run|-n)
            DRY_RUN="true"
            shift
            ;;
        --skip-assets)
            SKIP_ASSETS="true"
            shift
            ;;
        --skip-build)
            SKIP_BUILD="true"
            shift
            ;;
        --skip-push)
            SKIP_PUSH="true"
            shift
            ;;
        --skip-deploy)
            SKIP_DEPLOY="true"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Configuration with defaults
DOCKER_REGISTRY="${DOCKER_REGISTRY:-snormore}"
DOCKER_IMAGE="${DOCKER_IMAGE:-doublezero-lake}"
K8S_NAMESPACE="${K8S_NAMESPACE:-doublezero-data}"
K8S_DEPLOYMENT="${K8S_DEPLOYMENT:-lake-api}"
K8S_CONTAINER="${K8S_CONTAINER:-lake-api}"
export VITE_GOOGLE_CLIENT_ID="${VITE_GOOGLE_CLIENT_ID:-$GOOGLE_CLIENT_ID}"

# Build metadata
BUILD_COMMIT="$(git rev-parse --short HEAD)"
BUILD_DATE="$(date -u +%Y%m%d%H%M%S)"
BUILD_VERSION="v0.0.0-dev"
IMAGE_TAG="${DOCKER_REGISTRY}/${DOCKER_IMAGE}:${BUILD_COMMIT}"

echo "=== Lake API Deploy ==="
echo "Commit:     ${BUILD_COMMIT}"
echo "Image:      ${IMAGE_TAG}"
echo "Namespace:  ${K8S_NAMESPACE}"
echo "Deployment: ${K8S_DEPLOYMENT}"
echo ""

# Step 1: Upload assets to S3
if [[ -z "$SKIP_ASSETS" ]]; then
    if [[ -z "${ASSET_BUCKET:-}" ]]; then
        echo "Error: ASSET_BUCKET is required (or use --skip-assets)"
        exit 1
    fi
    if [[ -z "${VITE_GOOGLE_CLIENT_ID:-}" ]]; then
        echo "Error: VITE_GOOGLE_CLIENT_ID is required (or use --skip-assets)"
        exit 1
    fi
    echo "=== Uploading web assets to S3 ==="
    if [[ -n "$DRY_RUN" ]]; then
        echo "[dry-run] Would run: ./scripts/upload-web-assets.sh"
    else
        "$SCRIPT_DIR/upload-web-assets.sh"
    fi
    echo ""
fi

# Step 2: Build Docker image (uses pre-built web assets from step 1)
if [[ -z "$SKIP_BUILD" ]]; then
    echo "=== Building Docker image ==="

    DOCKER_BUILD_CMD=(
        docker build
        --build-arg "BUILD_DATE=${BUILD_DATE}"
        --build-arg "BUILD_VERSION=${BUILD_VERSION}"
        --build-arg "BUILD_COMMIT=${BUILD_COMMIT}"
        --platform linux/amd64
        -t "${IMAGE_TAG}"
        -f lake/Dockerfile
        .
    )

    if [[ -n "$DRY_RUN" ]]; then
        echo "[dry-run] Would run: ${DOCKER_BUILD_CMD[*]}"
    else
        cd "$REPO_ROOT"
        "${DOCKER_BUILD_CMD[@]}"
    fi
    echo ""
fi

# Step 3: Push Docker image
if [[ -z "$SKIP_PUSH" ]]; then
    echo "=== Pushing Docker image ==="
    if [[ -n "$DRY_RUN" ]]; then
        echo "[dry-run] Would run: docker push ${IMAGE_TAG}"
    else
        docker push "${IMAGE_TAG}"
    fi
    echo ""
fi

# Step 4: Deploy to Kubernetes
if [[ -z "$SKIP_DEPLOY" ]]; then
    echo "=== Deploying to Kubernetes ==="
    KUBECTL_CMD="kubectl -n ${K8S_NAMESPACE} set image deploy/${K8S_DEPLOYMENT} ${K8S_CONTAINER}=${IMAGE_TAG}"

    if [[ -n "$DRY_RUN" ]]; then
        echo "[dry-run] Would run: ${KUBECTL_CMD}"
        echo "[dry-run] Would run: kubectl -n ${K8S_NAMESPACE} rollout status deploy/${K8S_DEPLOYMENT}"
    else
        $KUBECTL_CMD
        echo ""
        echo "=== Waiting for rollout ==="
        kubectl -n "${K8S_NAMESPACE}" rollout status deploy/"${K8S_DEPLOYMENT}"
    fi
    echo ""
fi

echo "=== Done ==="
echo "Image: ${IMAGE_TAG}"
