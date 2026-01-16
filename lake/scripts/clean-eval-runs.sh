#!/bin/bash
# Clean up old eval run directories
# Usage: ./scripts/clean-eval-runs.sh [--keep N] [--dry-run]

set -e

KEEP=5
DRY_RUN=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --keep|-k)
            KEEP="$2"
            shift 2
            ;;
        --dry-run|-n)
            DRY_RUN=true
            shift
            ;;
        --all|-a)
            KEEP=0
            shift
            ;;
        --help|-h)
            echo "Usage: $0 [--keep N] [--dry-run] [--all]"
            echo ""
            echo "Options:"
            echo "  --keep, -k N    Keep the N most recent runs (default: 5)"
            echo "  --dry-run, -n   Show what would be deleted without deleting"
            echo "  --all, -a       Delete all runs (same as --keep 0)"
            echo "  --help, -h      Show this help"
            exit 0
            ;;
        *)
            echo "Unknown option: $1"
            exit 1
            ;;
    esac
done

EVAL_RUNS_DIR="eval-runs"

if [[ ! -d "$EVAL_RUNS_DIR" ]]; then
    echo "No eval-runs directory found"
    exit 0
fi

# Get all run directories sorted by name (timestamp format ensures correct order)
DIRS=($(ls -1 "$EVAL_RUNS_DIR" 2>/dev/null | sort -r))
TOTAL=${#DIRS[@]}

if [[ $TOTAL -eq 0 ]]; then
    echo "No eval runs found"
    exit 0
fi

TO_DELETE=$((TOTAL - KEEP))
if [[ $TO_DELETE -le 0 ]]; then
    echo "Found $TOTAL runs, keeping $KEEP - nothing to delete"
    exit 0
fi

echo "Found $TOTAL runs, keeping $KEEP most recent, deleting $TO_DELETE"

DELETED=0
for ((i=KEEP; i<TOTAL; i++)); do
    DIR="${DIRS[$i]}"
    if [[ "$DRY_RUN" == "true" ]]; then
        echo "Would delete: $EVAL_RUNS_DIR/$DIR"
    else
        rm -rf "$EVAL_RUNS_DIR/$DIR"
        echo "Deleted: $DIR"
    fi
    ((DELETED++))
done

if [[ "$DRY_RUN" == "true" ]]; then
    echo "Dry run complete - would delete $DELETED runs"
else
    echo "Deleted $DELETED runs"
fi
