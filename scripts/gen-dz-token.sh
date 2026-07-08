#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
    echo "Usage: $0 <solana-keypair-file>"
    exit 1
fi

KEYPAIR_FILE="$1"

if [[ ! -f "$KEYPAIR_FILE" ]]; then
    echo "File not found: $KEYPAIR_FILE"
    exit 1
fi

TOKEN=$(
python3 - "$KEYPAIR_FILE" <<'PY'
import json
import base64
import sys

with open(sys.argv[1], "r") as f:
    key = json.load(f)

raw = bytes(key)

token = base64.urlsafe_b64encode(raw).decode("ascii").rstrip("=")

print(f"DZ_{token}")
PY
)

echo "$TOKEN"
