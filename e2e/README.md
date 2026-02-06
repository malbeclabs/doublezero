# E2E Tests

## Backward Compatibility Test

Tests that older CLI versions (back to the onchain program's `min_compatible_version`) can perform all read and write operations against the current program.

The test clones account state from live environments, upgrades the program in-place with the current branch's build, then installs each old CLI version via Cloudsmith and runs the full workflow.

### Running

```bash
# Default: runs against testnet and mainnet-beta.
go test -tags e2e -run TestE2E_BackwardCompatibility -v -count=1 ./e2e/...

# Run against a single environment.
DZ_COMPAT_CLONE_ENV=mainnet-beta go test -tags e2e -run TestE2E_BackwardCompatibility -v -count=1 ./e2e/...

# Run against multiple specific environments (comma-separated).
DZ_COMPAT_CLONE_ENV=testnet,mainnet-beta,devnet go test -tags e2e -run TestE2E_BackwardCompatibility -v -count=1 ./e2e/...

# Override the minimum compatible version (useful after bumping min_compatible_version).
DZ_COMPAT_MIN_VERSION=0.8.1 go test -tags e2e -run TestE2E_BackwardCompatibility -v -count=1 ./e2e/...

# Limit to N most recent versions (useful for quick checks).
DZ_COMPAT_MAX_NUM_VERSIONS=2 go test -tags e2e -run TestE2E_BackwardCompatibility -v -count=1 ./e2e/...

# Keep containers alive after test for debugging.
TESTCONTAINERS_RYUK_DISABLED=true go test -tags e2e -run TestE2E_BackwardCompatibility -v -count=1 ./e2e/...
```

### Known Incompatibilities

Some CLI commands have known incompatibilities with newer program versions due to Borsh struct changes. These are documented in `knownIncompatibilities` at the top of `compatibility_test.go`. Known incompatible commands report `KNOWN_FAIL` instead of `FAIL` and don't cause the test to fail.

Current known incompatibilities:
- `write/multicast_group_create` - incompatible before v0.8.1 (Borsh struct changed: `index` and `bump_seed` fields were removed)

When adding new incompatibilities, document the reason and set the minimum compatible version. Remove entries when `min_compatible_version` is bumped past them.

### Example Output

<details>
<summary>Combined Compatibility Results (testnet + mainnet-beta)</summary>

```
=== testnet: Compatibility Matrix (Summary) ===

v0.8.2       ALL PASSED (35 passed)
v0.8.3       ALL PASSED (35 passed)
v0.8.4       ALL PASSED (35 passed)

=== testnet: Compatibility Matrix (Detail) ===

                                         v0.8.2     v0.8.3     v0.8.4
read/global_config_get                   PASS        PASS        PASS
read/location_list                       PASS        PASS        PASS
read/multicast_group_list                PASS        PASS        PASS
read/exchange_list                       PASS        PASS        PASS
read/link_list                           PASS        PASS        PASS
read/user_list                           PASS        PASS        PASS
read/device_list                         PASS        PASS        PASS
write/contributor_create                 PASS        PASS        PASS
write/location_create                    PASS        PASS        PASS
write/exchange_create                    PASS        PASS        PASS
write/device_create                      PASS        PASS        PASS
write/device_create_2                    PASS        PASS        PASS
write/device_interface_create            PASS        PASS        PASS
write/device_interface_create_2          PASS        PASS        PASS
write/device_interface_set_unlinked      PASS        PASS        PASS
write/device_interface_set_unlinked_2    PASS        PASS        PASS
write/link_create_wan                    PASS        PASS        PASS
write/multicast_group_create             PASS        PASS        PASS
write/location_update                    PASS        PASS        PASS
write/exchange_update                    PASS        PASS        PASS
write/contributor_update                 PASS        PASS        PASS
write/device_update                      PASS        PASS        PASS
write/cloned_location_update             PASS        PASS        PASS
write/cloned_exchange_update             PASS        PASS        PASS
write/device_list_verify                 PASS        PASS        PASS
write/link_list_verify                   PASS        PASS        PASS
write/link_wait_activated                PASS        PASS        PASS
write/link_delete                        PASS        PASS        PASS
write/iface_wait_unlinked                PASS        PASS        PASS
write/device_interface_delete            PASS        PASS        PASS
write/device_interface_delete_2          PASS        PASS        PASS
write/iface_wait_removed                 PASS        PASS        PASS
write/iface_wait_removed_2               PASS        PASS        PASS
write/device_delete                      PASS        PASS        PASS
write/device_delete_2                    PASS        PASS        PASS

=== mainnet-beta: Compatibility Matrix (Summary) ===

v0.7.1       ALL PASSED (34 passed, 1 known incompatible)
v0.7.2       ALL PASSED (34 passed, 1 known incompatible)
v0.8.0       ALL PASSED (34 passed, 1 known incompatible)
v0.8.1       ALL PASSED (35 passed)

=== mainnet-beta: Compatibility Matrix (Detail) ===

                                         v0.7.1     v0.7.2     v0.8.0     v0.8.1
read/global_config_get                   PASS        PASS        PASS        PASS
read/exchange_list                       PASS        PASS        PASS        PASS
read/multicast_group_list                PASS        PASS        PASS        PASS
read/location_list                       PASS        PASS        PASS        PASS
read/link_list                           PASS        PASS        PASS        PASS
read/device_list                         PASS        PASS        PASS        PASS
read/user_list                           PASS        PASS        PASS        PASS
write/contributor_create                 PASS        PASS        PASS        PASS
write/location_create                    PASS        PASS        PASS        PASS
write/exchange_create                    PASS        PASS        PASS        PASS
write/device_create                      PASS        PASS        PASS        PASS
write/device_create_2                    PASS        PASS        PASS        PASS
write/device_interface_create            PASS        PASS        PASS        PASS
write/device_interface_create_2          PASS        PASS        PASS        PASS
write/device_interface_set_unlinked      PASS        PASS        PASS        PASS
write/device_interface_set_unlinked_2    PASS        PASS        PASS        PASS
write/link_create_wan                    PASS        PASS        PASS        PASS
write/multicast_group_create             KNOWN_FAIL  KNOWN_FAIL  KNOWN_FAIL  PASS
write/location_update                    PASS        PASS        PASS        PASS
write/exchange_update                    PASS        PASS        PASS        PASS
write/contributor_update                 PASS        PASS        PASS        PASS
write/device_update                      PASS        PASS        PASS        PASS
write/cloned_location_update             PASS        PASS        PASS        PASS
write/cloned_exchange_update             PASS        PASS        PASS        PASS
write/device_list_verify                 PASS        PASS        PASS        PASS
write/link_list_verify                   PASS        PASS        PASS        PASS
write/link_wait_activated                PASS        PASS        PASS        PASS
write/link_delete                        PASS        PASS        PASS        PASS
write/iface_wait_unlinked                PASS        PASS        PASS        PASS
write/device_interface_delete            PASS        PASS        PASS        PASS
write/device_interface_delete_2          PASS        PASS        PASS        PASS
write/iface_wait_removed                 PASS        PASS        PASS        PASS
write/iface_wait_removed_2               PASS        PASS        PASS        PASS
write/device_delete                      PASS        PASS        PASS        PASS
write/device_delete_2                    PASS        PASS        PASS        PASS
```

</details>
