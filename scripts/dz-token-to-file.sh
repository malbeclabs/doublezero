#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 2 ]]; then
    echo "Usage: $0 <DZ_TOKEN> <output_file>"
    exit 1
fi

TOKEN="$1"
OUTPUT_FILE="$2"

if [[ "$TOKEN" != DZ_* ]]; then
    echo "Error: token must start with DZ_"
    exit 1
fi

B64URL="${TOKEN#DZ_}"

python3 - "$B64URL" "$OUTPUT_FILE" <<'PY'
import base64
import json
import sys

token = sys.argv[1]
output_file = sys.argv[2]

# Restore Base64 padding
padding = "=" * ((4 - len(token) % 4) % 4)

try:
    raw = base64.urlsafe_b64decode(token + padding)
except Exception as e:
    print(f"Invalid token: {e}", file=sys.stderr)
    sys.exit(1)

if len(raw) != 64:
    print(
        f"Invalid Solana secret key length: {len(raw)} bytes (expected 64)",
        file=sys.stderr
    )
    sys.exit(1)

with open(output_file, "w") as f:
    json.dump(list(raw), f)

print(f"Wrote Solana keypair to {output_file}")
PY

chmod 600 "$OUTPUT_FILE"
