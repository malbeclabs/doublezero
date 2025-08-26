package config

const (
	// DefaultMaxTunnelSlots is the default maximum number of tunnels to provision on a given device
	DefaultMaxTunnelSlots = 128
	// StartUserTunnelNum is the starting tunnel number for user tunnels
	StartUserTunnelNum = 500
	EndUserTunnelNum   = StartUserTunnelNum + DefaultMaxTunnelSlots - 1
)
