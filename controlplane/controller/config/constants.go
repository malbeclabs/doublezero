package config

const (
	// StartUserTunnelNum is the starting tunnel number for user tunnels
	StartUserTunnelNum = 500

	// DefaultMaxUserTunnelSlots is the default maximum number of user tunnel slots
	// per device. Controllers may override this at runtime via the
	// --max-user-tunnel-slots flag (see cmd/controller). The Arista EOS hard cap
	// is 1024.
	DefaultMaxUserTunnelSlots = 128
)
