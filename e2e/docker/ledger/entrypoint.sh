#!/usr/bin/env bash
set -euo pipefail

# Build additional flags for solana-test-validator from environment variables.
extra_args=""

ACCOUNTS_DIR="/tmp/fork-accounts"

# Support forking program accounts from a remote cluster.
# This fetches all accounts owned by each program via getProgramAccounts RPC,
# writes them as JSON files, and loads them via --account-dir. The program binary
# is deployed separately via --upgradeable-program with a custom upgrade authority.
#
# CLONE_FROM_URL: the RPC URL to fetch from (e.g., mainnet-beta)
# CLONE_PROGRAM_IDS: comma-separated list of program IDs to fetch accounts for
if [ -n "${CLONE_FROM_URL:-}" ] && [ -n "${CLONE_PROGRAM_IDS:-}" ]; then
  mkdir -p "${ACCOUNTS_DIR}"
  IFS=',' read -ra PROGRAM_IDS <<< "$CLONE_PROGRAM_IDS"
  for pid in "${PROGRAM_IDS[@]}"; do
    echo "==> Fetching accounts for program ${pid} from ${CLONE_FROM_URL}"
    # Fetch all accounts owned by the program via getProgramAccounts.
    response=$(curl -s "${CLONE_FROM_URL}" -X POST -H "Content-Type: application/json" \
      -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"getProgramAccounts\",\"params\":[\"${pid}\",{\"encoding\":\"base64\"}]}")

    # Parse and write each account as a JSON file in the format expected by --account-dir.
    echo "${response}" | python3 -c "
import json, sys
data = json.load(sys.stdin)
accounts = data.get('result', [])
for entry in accounts:
    pubkey = entry['pubkey']
    acct = entry['account']
    out = {
        'pubkey': pubkey,
        'account': {
            'lamports': acct['lamports'],
            'data': acct['data'],
            'owner': acct['owner'],
            'executable': acct['executable'],
            'rentEpoch': acct['rentEpoch'],
            'space': acct['space']
        }
    }
    with open('${ACCOUNTS_DIR}/' + pubkey + '.json', 'w') as f:
        json.dump(out, f)
print(f'Wrote {len(accounts)} accounts for program ${pid}')
"
  done
  extra_args="${extra_args} --account-dir ${ACCOUNTS_DIR}"
fi

# Patch the GlobalState account to add a pubkey to the foundation_allowlist.
# This allows a test manager to execute write operations against cloned mainnet state.
#
# PATCH_GLOBALSTATE_AUTHORITY: base58 pubkey to add to the foundation_allowlist
if [ -n "${PATCH_GLOBALSTATE_AUTHORITY:-}" ] && [ -d "${ACCOUNTS_DIR}" ]; then
  echo "==> Patching GlobalState foundation_allowlist with ${PATCH_GLOBALSTATE_AUTHORITY}"
  python3 -c "
import json, base64, struct, os, sys

authority_b58 = '${PATCH_GLOBALSTATE_AUTHORITY}'
accounts_dir = '${ACCOUNTS_DIR}'

# Base58 decode.
ALPHABET = b'123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz'
def b58decode(s):
    n = 0
    for c in s.encode():
        n = n * 58 + ALPHABET.index(c)
    result = n.to_bytes((n.bit_length() + 7) // 8, 'big')
    # Count leading 1s for zero bytes.
    pad = len(s) - len(s.lstrip('1'))
    return b'\x00' * pad + result

authority_bytes = b58decode(authority_b58)
assert len(authority_bytes) == 32, f'Expected 32 bytes, got {len(authority_bytes)}'

patched = False
for fname in os.listdir(accounts_dir):
    if not fname.endswith('.json'):
        continue
    fpath = os.path.join(accounts_dir, fname)
    with open(fpath) as f:
        acct = json.load(f)

    data_b64 = acct['account']['data'][0]
    data = bytearray(base64.b64decode(data_b64))

    # GlobalState account_type == 1 (first byte).
    if len(data) < 22 or data[0] != 1:
        continue

    # Borsh layout: account_type(1) + bump_seed(1) + account_index(16) + foundation_allowlist_len(4) + entries(32*N)
    list_len = struct.unpack_from('<I', data, 18)[0]
    if list_len == 0:
        continue

    # Append the authority pubkey to the foundation_allowlist.
    new_len = list_len + 1
    struct.pack_into('<I', data, 18, new_len)
    insert_offset = 22 + list_len * 32
    data[insert_offset:insert_offset] = authority_bytes

    # Also patch activator_authority_pk to the same authority.
    # Borsh layout after foundation_allowlist: _device_allowlist(Vec) + _user_allowlist(Vec) + activator_authority_pk(32)
    # After inserting into foundation_allowlist, recalculate offsets.
    offset = 18  # start of foundation_allowlist
    fa_len = struct.unpack_from('<I', data, offset)[0]
    offset += 4 + fa_len * 32  # skip foundation_allowlist
    da_len = struct.unpack_from('<I', data, offset)[0]
    offset += 4 + da_len * 32  # skip _device_allowlist
    ua_len = struct.unpack_from('<I', data, offset)[0]
    offset += 4 + ua_len * 32  # skip _user_allowlist
    # offset now points to activator_authority_pk
    data[offset:offset+32] = authority_bytes

    # Update the base64 data and space.
    acct['account']['data'][0] = base64.b64encode(bytes(data)).decode()
    acct['account']['space'] = len(data)

    with open(fpath, 'w') as f:
        json.dump(acct, f)

    print(f'Patched GlobalState account {fname}: added authority to foundation_allowlist (now {new_len} entries) and set activator_authority_pk')
    patched = True
    break

if not patched:
    print('WARNING: No GlobalState account found to patch', file=sys.stderr)
"
fi

# Support deploying upgraded programs at startup with a specific upgrade authority.
# UPGRADE_PROGRAM_ID: program ID to upgrade
# UPGRADE_PROGRAM_SO: path to the .so file inside the container
# UPGRADE_AUTHORITY: pubkey of the upgrade authority
if [ -n "${UPGRADE_PROGRAM_ID:-}" ] && [ -n "${UPGRADE_PROGRAM_SO:-}" ] && [ -n "${UPGRADE_AUTHORITY:-}" ]; then
  extra_args="${extra_args} --upgradeable-program ${UPGRADE_PROGRAM_ID} ${UPGRADE_PROGRAM_SO} ${UPGRADE_AUTHORITY}"
  echo "==> Deploying upgraded program ${UPGRADE_PROGRAM_ID} with authority ${UPGRADE_AUTHORITY}"
fi

# Start the solana validator with some noisy output filtered out.
# Configuration and data available at /test-ledger
# Validator logging available at /test-ledger/validator.log
script -q -c "solana-test-validator ${extra_args} 2>&1" /dev/null | grep --line-buffered -v "Processed Slot: "
