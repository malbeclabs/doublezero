package config

const (
	// StartUserTunnelNum is the starting tunnel number for user tunnels
	StartUserTunnelNum = 1

	// DefaultMaxUserTunnelSlots is the default maximum number of user tunnel slots
	// per device. Controllers may override this at runtime via the
	// --max-user-tunnel-slots flag (see cmd/controller).
	DefaultMaxUserTunnelSlots = 128
)
