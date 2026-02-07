package server

import (
	"context"
	"errors"
	"time"

	awssigner "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
)

const (
	defaultTimeSkew                      = 5 * time.Minute
	defaultPresignTTL                    = 15 * time.Minute
	defaultServiceabilityRefreshInterval = 1 * time.Minute
	defaultShutdownTimeout               = 10 * time.Second
	defaultMaxBodySize                   = 1 << 20 // 1 MiB
)

var (
	defaultStateToCollectShowCommands = map[string]string{
		"snmp-mib-ifmib-ifindex": "show snmp mib ifmib ifindex",
		"isis-database-detail":   "show isis database detail",
	}
	defaultStateToCollectCustom = []string{
		"bgp-sockets",
	}
)

type PresignClient interface {
	PresignPutObject(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error)
}

type ServiceabilityRPC interface {
	GetProgramData(ctx context.Context) (*serviceability.ProgramData, error)
}

type Config struct {
	Presign           PresignClient
	BucketName        string
	BucketPathPrefix  string
	ServiceabilityRPC ServiceabilityRPC

	// Optional configuration.
	Clock                         clockwork.Clock
	AuthTimeSkew                  time.Duration
	PresignTTL                    time.Duration
	ServiceabilityRefreshInterval time.Duration
	ShutdownTimeout               time.Duration
	MaxBodySize                   int64
	StateToCollectShowCommands    map[string]string
	StateToCollectCustom          []string
}

func (c *Config) Validate() error {
	if c.Presign == nil {
		return errors.New("presign client is required")
	}
	if c.BucketName == "" {
		return errors.New("bucket name is required")
	}
	if c.ServiceabilityRPC == nil {
		return errors.New("serviceability RPC is required")
	}

	// Optional configuration.
	if c.Clock == nil {
		c.Clock = clockwork.NewRealClock()
	}
	if c.AuthTimeSkew <= 0 {
		c.AuthTimeSkew = defaultTimeSkew
	}
	if c.PresignTTL <= 0 {
		c.PresignTTL = defaultPresignTTL
	}
	if c.ServiceabilityRefreshInterval <= 0 {
		c.ServiceabilityRefreshInterval = defaultServiceabilityRefreshInterval
	}
	if c.ShutdownTimeout <= 0 {
		c.ShutdownTimeout = defaultShutdownTimeout
	}
	if c.MaxBodySize <= 0 {
		c.MaxBodySize = defaultMaxBodySize
	}
	if c.StateToCollectShowCommands == nil {
		c.StateToCollectShowCommands = defaultStateToCollectShowCommands
	}
	if c.StateToCollectCustom == nil {
		c.StateToCollectCustom = defaultStateToCollectCustom
	}
	return nil
}
