package telemetry

import (
	"sync"
	"time"
)

// RTTSample represents a single round-trip time measurement
type RTTSample struct {
	// RTT in microseconds
	RTTMicroseconds uint32
	// Timestamp when the sample was taken
	Timestamp time.Time
	// PacketID for matching requests/responses
	PacketID uint32
}

// LinkSamples represents RTT samples for a specific link
type LinkSamples struct {
	// DeviceAPubkey is the source device public key
	DeviceAPubkey string
	// DeviceZPubkey is the destination device public key
	DeviceZPubkey string
	// LinkPubkey identifies the specific link (all 1s for internet data)
	LinkPubkey string
	// LocationAPubkey is the source location
	LocationAPubkey string
	// LocationZPubkey is the destination location
	LocationZPubkey string
	// Samples collected for this link
	Samples []RTTSample
	// StartTimestamp marks when this accumulation window started
	StartTimestamp time.Time
	// NextSampleIndex for tracking position in fixed-size buffer
	NextSampleIndex uint32
	// SamplingIntervalMicroseconds between measurements
	SamplingIntervalMicroseconds uint64
}

// PingRequest represents a UDP ping request packet
type PingRequest struct {
	// PacketID uniquely identifies this ping
	PacketID uint32
	// Timestamp when the request was sent (microseconds since epoch)
	TimestampMicroseconds uint64
	// SourceDevice public key
	SourceDevice string
	// Padding to reach 2048 bytes total packet size
	Padding [2032]byte // 2048 - 4 (PacketID) - 8 (Timestamp) - 4 (estimated header overhead)
}

// PingResponse represents a UDP ping response packet
type PingResponse struct {
	// PacketID matches the request
	PacketID uint32
	// RequestTimestamp from the original request (microseconds since epoch)
	RequestTimestampMicroseconds uint64
	// ResponseTimestamp when the response was sent (microseconds since epoch)
	ResponseTimestampMicroseconds uint64
	// Padding to reach 2048 bytes total packet size
	Padding [2024]byte // 2048 - 4 (PacketID) - 8 (RequestTimestamp) - 8 (ResponseTimestamp) - 4 (estimated header overhead)
}

// CollectorConfig holds configuration for the telemetry collector
type CollectorConfig struct {
	// LocalDevicePubkey is this device's public key
	LocalDevicePubkey string
	// LocalLocationPubkey is this device's location
	LocalLocationPubkey string
	// ListenPort for UDP ping service
	ListenPort int
	// SamplingIntervalSeconds between RTT measurements
	SamplingIntervalSeconds int
	// SubmissionIntervalSeconds for batching samples
	SubmissionIntervalSeconds int
	// StoragePath for local persistence
	StoragePath string
	// MaxSamplesPerLink before rotation
	MaxSamplesPerLink int
	// EnableInternetProbes to measure internet latency
	EnableInternetProbes bool
}

// PeerDevice represents a remote device to measure RTT to
type PeerDevice struct {
	// DevicePubkey identifies the peer
	DevicePubkey string
	// LocationPubkey of the peer
	LocationPubkey string
	// IP address of the peer (from BGP neighbor data)
	IP string
	// LinkPubkey for DZ link measurements
	LinkPubkey string
	// IsInternetPeer indicates if this is for internet measurements
	IsInternetPeer bool
}

// Collector manages RTT measurements and sample accumulation
type Collector struct {
	config     CollectorConfig
	peers      map[string]*PeerDevice  // key is device pubkey
	samples    map[string]*LinkSamples // key is "deviceA:deviceZ:link"
	mu         sync.RWMutex
	stopCh     chan struct{}
	packetPool sync.Pool
	storage    Storage
	nextID     uint32
	idMu       sync.Mutex
}

// Storage interface for persisting telemetry data
type Storage interface {
	// SaveSamples persists link samples to disk
	SaveSamples(linkKey string, samples *LinkSamples) error
	// LoadSamples retrieves persisted samples
	LoadSamples() (map[string]*LinkSamples, error)
	// RotateSamples archives old samples
	RotateSamples(linkKey string) error
}

// TelemetrySubmitter interface for future smart contract integration
type TelemetrySubmitter interface {
	// SubmitSamples sends accumulated samples to the telemetry smart contract
	SubmitSamples(epoch uint64, samples map[string]*LinkSamples) error
}
