#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
workspace_dir="$(dirname "$script_dir")"

target_test="${1-}"

export TESTCONTAINERS_RYUK_DISABLED="${TESTCONTAINERS_RYUK_DISABLED:-true}"

max_runs="${MAX_RUNS:-0}"  # 0 = infinite

counter=0
start_ts=$(date +"%Y-%m-%dT%H:%M:%S%z")
interrupted=0
last_fail_code=""

ts() { date +"%Y-%m-%dT%H:%M:%S%z"; }

on_int() {
  interrupted=1
  echo
  echo "⛔ Interrupted after ${counter} successful run(s)."
  exit 130
}

on_exit() {
  echo "Started: ${start_ts}"
  echo "Ended:   $(ts)"
  echo "Total successful runs: ${counter}"
  if [ "$interrupted" -eq 1 ]; then
    echo "Exit reason: interrupted (SIGINT/SIGTERM)"
  elif [ -n "${last_fail_code}" ]; then
    echo "Exit reason: command failed with status ${last_fail_code}"
  else
    echo "Exit reason: completed"
  fi
}

trap on_int INT TERM
trap on_exit EXIT

while :; do
  echo "[$(ts)] Cleaning up existing testcontainers..."
  set +e
  "${workspace_dir}/dev/e2e-clean.sh"
  clean_rc=$?
  set -e
  if [ $clean_rc -ne 0 ]; then
    echo "[$(ts)] Cleanup returned $clean_rc (ignored)"
  fi

  if [ -n "$target_test" ]; then
    echo "[$(ts)] Running test \"$target_test\" (run $((counter+1)))"
  else
    echo "[$(ts)] Running all tests (run $((counter+1)))"
  fi

  set +e
  "${workspace_dir}/dev/e2e-test.sh" "${target_test}"
  ret_val=$?
  set -e

  if [ $ret_val -ne 0 ]; then
    last_fail_code="$ret_val"
    echo "[$(ts)] ❌ Command failed with status $ret_val after ${counter} successful run(s)"
    break
  fi

  counter=$((counter + 1))
  echo "[$(ts)] ✅ Test run ${counter} completed"

  if [ "$max_runs" -gt 0 ] && [ "$counter" -ge "$max_runs" ]; then
    echo "[$(ts)] Reached MAX_RUNS=${max_runs}. Done."
    break
  fi
done
