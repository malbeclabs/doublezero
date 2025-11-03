#!/bin/bash
set -euo pipefail


# Usage: entrypoint.sh [-v|--verbose] <environment> <current_controller_addr>
VERBOSE=0
if [[ "$1" == "-v" || "$1" == "--verbose" ]]; then
  VERBOSE=1
  shift
fi

if [ "$#" -ne 2 ]; then
  echo "Usage: $0 [-v|--verbose] <environment> <current_controller_addr>"
  exit 1
fi

OUTDIR="/var/tmp/devices_out"
ENV="$1"
CURR_ADDR="$2"

mkdir -p "$OUTDIR"

cd "$OUTDIR"
if [ ! -d .git ]; then
  git init > /dev/null 2>&1
fi
cd - > /dev/null

case "$ENV" in
  devnet|testnet|mainnet)
    ;;
  *)
    echo "Unknown environment: $ENV"
    exit 1
    ;;
esac

if [ "$VERBOSE" -eq 1 ]; then
  /app/cdiff -out "$OUTDIR" -env "$ENV" -controller "$CURR_ADDR"
else
  if ! /app/cdiff -out "$OUTDIR" -env "$ENV" -controller "$CURR_ADDR" > /dev/null 2>&1; then
    echo "Error: cdiff failed for current controller ($CURR_ADDR)"
    exit 1
  fi
fi

cd "$OUTDIR"
git add . > /dev/null 2>&1
git commit -m "current production configs" > /dev/null 2>&1 || true
cd - > /dev/null

if [ "$VERBOSE" -eq 1 ]; then
  /app/controller start -env "$ENV" -listen-port 7000 &
else
  /app/controller start -env "$ENV" -listen-port 7000 > /dev/null 2>&1 &
fi
CONTROLLER_PID=$!
sleep 3

if [ "$VERBOSE" -eq 1 ]; then
  /app/cdiff -out "$OUTDIR" -env "$ENV" -controller "localhost:7000"
else
  if ! /app/cdiff -out "$OUTDIR" -env "$ENV" -controller "localhost:7000" > /dev/null 2>&1; then
    echo "Error: cdiff failed for local controller (localhost:7000)"
    kill $CONTROLLER_PID
    wait $CONTROLLER_PID 2>/dev/null || true
    exit 1
  fi
fi

cd "$OUTDIR"
git diff -U3 | awk '/^diff --git /{print "";} {print;}'
cd - > /dev/null



kill $CONTROLLER_PID
wait $CONTROLLER_PID 2>/dev/null || true
