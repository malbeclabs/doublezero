package serviceability

// FeatureFlag mirrors the FeatureFlag enum in
// smartcontract/programs/doublezero-serviceability/src/state/feature_flags.rs.
// The values are bit positions in GlobalState.feature_flags (a u128), so they
// must stay in lockstep with the Rust enum.
type FeatureFlag uint8

const (
	// FeatureOnChainAllocationDeprecated is bit 0, formerly OnChainAllocation and
	// now always-on. Reserved; never reuse it for a new flag.
	FeatureOnChainAllocationDeprecated FeatureFlag = 0
	// FeatureRequirePermissionAccounts is bit 1. When set, authorization requires
	// a Permission account and the GlobalState allowlist fallback is disabled.
	FeatureRequirePermissionAccounts FeatureFlag = 1
	// FeatureRequireIPOwnershipProof is bit 2 (RFC-27). When set, user creation
	// requires a valid IpOwnershipProof signed by
	// GlobalState.ip_verifier_authority_pk.
	FeatureRequireIPOwnershipProof FeatureFlag = 2
)

// String returns the flag's canonical name, matching the Rust Display impl and
// the string the CLI accepts.
func (f FeatureFlag) String() string {
	switch f {
	case FeatureOnChainAllocationDeprecated:
		return "onchain-allocation-deprecated"
	case FeatureRequirePermissionAccounts:
		return "require-permission-accounts"
	case FeatureRequireIPOwnershipProof:
		return "require-ip-ownership-proof"
	default:
		return "unknown"
	}
}

// Mask returns the bitmask for the flag.
func (f FeatureFlag) Mask() uint64 {
	return 1 << uint64(f)
}

// Lo64 returns the low 64 bits of the u128.
//
// ByteReader.ReadU128 fills Uint128.High from the FIRST eight encoded bytes.
// Borsh writes a u128 little-endian, so those first eight bytes are the LOW
// half of the value — the two fields are named the wrong way round. Existing
// callers and fixtures bake in that convention (see client_test.go, which
// stores small account indexes in High), so this accessor isolates the wart in
// one place rather than renaming the fields.
//
// Do not "correct" this to u.Low without fixing ReadU128 and every caller
// together.
func (u Uint128) Lo64() uint64 {
	return u.High
}

// IsFeatureEnabled reports whether the given feature flag is set in global
// state. All defined flags live in the low 64 bits.
func (gs *GlobalState) IsFeatureEnabled(f FeatureFlag) bool {
	if gs == nil {
		return false
	}
	return gs.FeatureFlags.Lo64()&f.Mask() != 0
}
