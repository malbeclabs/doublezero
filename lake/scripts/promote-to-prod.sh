#!/bin/bash
set -euo pipefail

# Promote a lake image from staging to prod (or between any tags)
# Usage: ./promote-to-prod.sh [options] [source_tag] [target_tag]

IMAGE="ghcr.io/malbeclabs/doublezero-lake"
DRY_RUN=false
YES=false

usage() {
    echo "Usage: $0 [options] [source_tag] [target_tag]"
    echo ""
    echo "Promotes a lake Docker image from one tag to another."
    echo "Default: staging → prod"
    echo ""
    echo "Options:"
    echo "  -n, --dry-run  Show what would be done without making changes"
    echo "  -y, --yes      Skip confirmation prompt"
    echo "  -h, --help     Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                    # promote staging → prod (with confirmation)"
    echo "  $0 -y                 # promote staging → prod (no confirmation)"
    echo "  $0 --dry-run          # show what would happen"
    echo "  $0 main prod          # promote main → prod"
}

# Parse flags
while [[ $# -gt 0 ]]; do
    case $1 in
        -n|--dry-run)
            DRY_RUN=true
            shift
            ;;
        -y|--yes)
            YES=true
            shift
            ;;
        -h|--help)
            usage
            exit 0
            ;;
        -*)
            echo "Unknown option: $1"
            usage
            exit 1
            ;;
        *)
            break
            ;;
    esac
done

SOURCE_TAG="${1:-staging}"
TARGET_TAG="${2:-prod}"

echo "Promote: ${IMAGE}:${SOURCE_TAG} → ${IMAGE}:${TARGET_TAG}"
echo ""

if $DRY_RUN; then
    echo "[dry-run] Would pull ${IMAGE}:${SOURCE_TAG}"
    echo "[dry-run] Would tag as ${IMAGE}:${TARGET_TAG}"
    echo "[dry-run] Would push ${IMAGE}:${TARGET_TAG}"
    echo ""
    echo "Run without --dry-run to execute."
    exit 0
fi

if ! $YES; then
    read -p "Continue? [y/N] " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "Aborted."
        exit 1
    fi
fi

# Ensure we're logged in to GHCR
if ! docker pull "${IMAGE}:${SOURCE_TAG}" 2>/dev/null; then
    echo "Login to GHCR required..."
    echo "$(gh auth token)" | docker login ghcr.io -u malbeclabs --password-stdin
    docker pull "${IMAGE}:${SOURCE_TAG}"
fi

docker tag "${IMAGE}:${SOURCE_TAG}" "${IMAGE}:${TARGET_TAG}"
docker push "${IMAGE}:${TARGET_TAG}"

echo ""
echo "✅ Promoted successfully"
echo ""
echo "ArgoCD will pick up the new digest automatically."
echo "To force immediate sync: kubectl -n argocd patch app lake-prod --type merge -p '{\"metadata\":{\"annotations\":{\"argocd.argoproj.io/refresh\":\"hard\"}}}'"
