package geolocation

// PDA seeds for geolocation program
const (
	SeedPrefix          = "doublezero"
	ProgramConfigSeed   = "programconfig"
	GeoProbeAccountSeed = "probe"
	GeolocationUserSeed = "geouser"
)

// Limits
const (
	MaxParentDevices = 5
	MaxCodeLength    = 32
	MaxTargets       = 4096
)

// CU budget covering AddTarget / RemoveTarget at MaxTargets. Both onchain handlers do
// an O(n) scan of existing targets that exhausts the default 200K CU limit well before
// MaxTargets (~743 in practice). Sized to match add_target_cu_benchmark.rs, which is
// also the per-tx ceiling.
const TargetMutationComputeUnitLimit uint32 = 1_400_000
