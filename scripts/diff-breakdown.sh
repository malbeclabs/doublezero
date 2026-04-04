#!/usr/bin/env bash
#
# scripts/diff-breakdown.sh — Categorize git diff between current branch and a base ref.
#
# Outputs a JSON object with categorized files and tallies, plus a pre-formatted
# markdown table. Designed to be consumed by Claude Code skills (diff-breakdown,
# pr-text, changelog) so the mechanical work isn't repeated by the LLM.
#
# Usage:
#   scripts/diff-breakdown.sh [base_ref]
#
# base_ref defaults to origin/main. The script runs `git fetch origin` first.

set -euo pipefail

BASE_REF="${1:-origin/main}"

# Fetch to ensure remote tracking is current.
git fetch origin --quiet 2>/dev/null

# --- Collect numstat --------------------------------------------------------

numstat=$(git diff "${BASE_REF}...HEAD" --numstat)

if [[ -z "$numstat" ]]; then
  echo '{"categories":{},"table":"No changes found.","unclassified":[]}'
  exit 0
fi

# --- Categorize files -------------------------------------------------------
# Categories: tests, fixtures, config, docs, generated, unclassified
# "unclassified" = files the caller (skill) must judge as scaffolding vs core logic.

declare -A cat_added cat_removed cat_files
for cat in tests fixtures config docs generated unclassified; do
  cat_added[$cat]=0
  cat_removed[$cat]=0
  cat_files[$cat]=0
done

# Collect file details for JSON output.
unclassified_json="["
first_unclassified=true
all_files_json="["
first_all=true

while IFS=$'\t' read -r added removed file; do
  # Binary files show as "-" in numstat.
  [[ "$added" == "-" ]] && added=0
  [[ "$removed" == "-" ]] && removed=0

  category="unclassified"

  # Order matters — first match wins.

  # Tests
  if [[ "$file" == *_test.go ]] || [[ "$file" == *_test.rs ]] || \
     [[ "$file" == *_test.py ]] || [[ "$file" == *.test.ts ]] || \
     [[ "$file" == *.test.js ]] || [[ "$file" == tests/* ]] || \
     [[ "$file" == */tests/* ]] || [[ "$file" == e2e/* ]]; then
    category="tests"

  # Fixtures/snapshots
  elif [[ "$file" == *fixtures/* ]] || [[ "$file" == *snapshots/* ]]; then
    category="fixtures"

  # Generated (before config, since go.sum and lock files overlap)
  elif [[ "$file" == *.pb.go ]] || [[ "$file" == *.pb.rs ]] || \
       [[ "$file" == Cargo.lock ]] || [[ "$file" == */Cargo.lock ]] || \
       [[ "$file" == go.sum ]] || [[ "$file" == */go.sum ]] || \
       [[ "$file" == bun.lockb ]] || [[ "$file" == */bun.lockb ]] || \
       [[ "$file" == package-lock.json ]] || [[ "$file" == */package-lock.json ]]; then
    category="generated"

  # Config/build
  elif [[ "$file" == Cargo.toml ]] || [[ "$file" == */Cargo.toml ]] || \
       [[ "$file" == go.mod ]] || [[ "$file" == */go.mod ]] || \
       [[ "$file" == Makefile ]] || [[ "$file" == */Makefile ]] || \
       [[ "$file" == *.toml ]] || [[ "$file" == *.yml ]] || \
       [[ "$file" == *.yaml ]] || [[ "$file" == Dockerfile ]] || \
       [[ "$file" == */Dockerfile ]] || [[ "$file" == *.lock ]]; then
    category="config"

  # Docs
  elif [[ "$file" == *.md ]] || [[ "$file" == rfcs/* ]] || [[ "$file" == */rfcs/* ]]; then
    category="docs"
  fi

  cat_added[$category]=$(( ${cat_added[$category]} + added ))
  cat_removed[$category]=$(( ${cat_removed[$category]} + removed ))
  cat_files[$category]=$(( ${cat_files[$category]} + 1 ))

  # Compute SHA256 of the file path (used for GitHub PR diff links).
  diff_hash=$(printf '%s' "$file" | shasum -a 256 | cut -d' ' -f1)
  escaped_file=$(printf '%s' "$file" | sed 's/\\/\\\\/g; s/"/\\"/g')

  # All files list.
  if $first_all; then
    first_all=false
  else
    all_files_json+=","
  fi
  all_files_json+="{\"file\":\"${escaped_file}\",\"added\":${added},\"removed\":${removed},\"category\":\"${category}\",\"diff_hash\":\"${diff_hash}\"}"

  if [[ "$category" == "unclassified" ]]; then
    if $first_unclassified; then
      first_unclassified=false
    else
      unclassified_json+=","
    fi
    unclassified_json+="{\"file\":\"${escaped_file}\",\"added\":${added},\"removed\":${removed},\"diff_hash\":\"${diff_hash}\"}"
  fi
done <<< "$numstat"

unclassified_json+="]"
all_files_json+="]"

# --- Build markdown table ---------------------------------------------------

total_added=0
total_removed=0
total_files=0

table="| Category          | Files | Lines (+/-)     | Net    |\n"
table+="|-------------------|-------|-----------------|--------|\n"

for cat in tests fixtures config docs generated unclassified; do
  files=${cat_files[$cat]}
  added=${cat_added[$cat]}
  removed=${cat_removed[$cat]}
  (( files == 0 )) && continue

  net=$(( added - removed ))
  total_added=$(( total_added + added ))
  total_removed=$(( total_removed + removed ))
  total_files=$(( total_files + files ))

  # Pretty-print category name.
  case $cat in
    tests)        label="Tests" ;;
    fixtures)     label="Fixtures" ;;
    config)       label="Config/build" ;;
    docs)         label="Docs" ;;
    generated)    label="Generated" ;;
    unclassified) label="Unclassified" ;;
  esac

  net_str=$(( net >= 0 )) && net_str="+${net}" || net_str="${net}"
  printf -v row "| %-17s | %5d | +%-5d / -%-5d | %+6d |" "$label" "$files" "$added" "$removed" "$net"
  table+="${row}\n"
done

total_net=$(( total_added - total_removed ))
printf -v total_row "| %-17s | %5d | +%-5d / -%-5d | %+6d |" "**Total**" "$total_files" "$total_added" "$total_removed" "$total_net"
table+="${total_row}"

# --- Detect PR URL ----------------------------------------------------------

pr_url=""
pr_number=""
if pr_json=$(gh pr view --json number,url 2>/dev/null); then
  pr_url=$(echo "$pr_json" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p')
  pr_number=$(echo "$pr_json" | sed -n 's/.*"number":\([0-9]*\).*/\1/p')
fi

# --- Output JSON ------------------------------------------------------------

cat <<ENDJSON
{
  "base_ref": "${BASE_REF}",
  "categories": {
    "tests":        {"files": ${cat_files[tests]},        "added": ${cat_added[tests]},        "removed": ${cat_removed[tests]}},
    "fixtures":     {"files": ${cat_files[fixtures]},     "added": ${cat_added[fixtures]},     "removed": ${cat_removed[fixtures]}},
    "config":       {"files": ${cat_files[config]},       "added": ${cat_added[config]},       "removed": ${cat_removed[config]}},
    "docs":         {"files": ${cat_files[docs]},         "added": ${cat_added[docs]},         "removed": ${cat_removed[docs]}},
    "generated":    {"files": ${cat_files[generated]},    "added": ${cat_added[generated]},    "removed": ${cat_removed[generated]}},
    "unclassified": {"files": ${cat_files[unclassified]}, "added": ${cat_added[unclassified]}, "removed": ${cat_removed[unclassified]}}
  },
  "all_files": ${all_files_json},
  "unclassified_files": ${unclassified_json},
  "total": {"files": ${total_files}, "added": ${total_added}, "removed": ${total_removed}, "net": ${total_net}},
  "pr_url": "${pr_url}",
  "pr_number": "${pr_number}",
  "table": "$(echo -e "$table" | sed 's/"/\\"/g')"
}
ENDJSON
