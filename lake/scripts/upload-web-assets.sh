#!/bin/bash
set -euo pipefail

# Upload web assets to S3 bucket for persistence across deploys
#
# Usage:
#   ./scripts/upload-web-assets.sh [--dry-run]
#
# Options:
#   --dry-run, -n    Show what would be uploaded without actually uploading
#
# Environment:
#   ASSET_BUCKET - S3 bucket name (required, e.g., "my-bucket")
#   ASSET_BUCKET_PREFIX - Optional prefix/path in bucket (default: "assets")
#
# Prerequisites:
#   - AWS CLI configured with credentials that can write to the bucket
#   - bun installed for building

DRY_RUN=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --dry-run|-n)
            DRY_RUN="--dryrun"
            shift
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
WEB_DIR="$ROOT_DIR/web"

if [[ -z "${ASSET_BUCKET:-}" ]]; then
    echo "Error: ASSET_BUCKET environment variable is required"
    echo "Example: ASSET_BUCKET=my-lake-assets ./scripts/upload-web-assets.sh"
    exit 1
fi

ASSET_BUCKET_PREFIX="${ASSET_BUCKET_PREFIX:-assets}"

echo "Building web app..."
cd "$WEB_DIR"
bun run build

if [[ -n "$DRY_RUN" ]]; then
    echo "Dry run: would upload assets to s3://${ASSET_BUCKET}/${ASSET_BUCKET_PREFIX}/"
else
    echo "Uploading assets to s3://${ASSET_BUCKET}/${ASSET_BUCKET_PREFIX}/"
fi

# Sync assets directory to S3
# --size-only: Skip files that match in size (hashed filenames mean same hash = same content)
# --cache-control: Set long cache since files are content-hashed
aws s3 sync \
    "$WEB_DIR/dist/assets/" \
    "s3://${ASSET_BUCKET}/${ASSET_BUCKET_PREFIX}/" \
    --size-only \
    --cache-control "public, max-age=31536000, immutable" \
    $DRY_RUN

if [[ -n "$DRY_RUN" ]]; then
    echo "Dry run complete. No files were uploaded."
else
    echo "Done. Assets uploaded to s3://${ASSET_BUCKET}/${ASSET_BUCKET_PREFIX}/"
fi
echo ""
echo "Configure your API with:"
echo "  ASSET_BUCKET_URL=https://${ASSET_BUCKET}.s3.amazonaws.com/${ASSET_BUCKET_PREFIX}"
