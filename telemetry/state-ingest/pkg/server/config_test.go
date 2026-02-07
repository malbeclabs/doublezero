package server

import (
	"context"
	"testing"
	"time"

	awssigner "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/jonboulle/clockwork"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/require"
)

func TestTelemetry_StateIngest_Config_Validate_RequiredFields(t *testing.T) {
	t.Parallel()

	okPresign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid"}, nil
		},
	}
	okRPC := mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	}

	type tc struct {
		name    string
		cfg     Config
		wantErr string
	}

	tests := []tc{
		{
			name: "missing presign",
			cfg: Config{
				BucketName:        "b",
				ServiceabilityRPC: okRPC,
			},
			wantErr: "presign client is required",
		},
		{
			name: "missing bucket name",
			cfg: Config{
				Presign:           okPresign,
				ServiceabilityRPC: okRPC,
			},
			wantErr: "bucket name is required",
		},
		{
			name: "missing serviceability rpc",
			cfg: Config{
				Presign:    okPresign,
				BucketName: "b",
			},
			wantErr: "serviceability RPC is required",
		},
		{
			name: "ok minimal",
			cfg: Config{
				Presign:           okPresign,
				BucketName:        "b",
				ServiceabilityRPC: okRPC,
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.cfg.Validate()
			if tt.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tt.wantErr)
			}
		})
	}
}

func TestTelemetry_StateIngest_Config_Validate_DefaultsApplied(t *testing.T) {
	t.Parallel()

	okPresign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid"}, nil
		},
	}
	okRPC := mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	}

	var cfg Config
	cfg.Presign = okPresign
	cfg.BucketName = "bucket-1"
	cfg.ServiceabilityRPC = okRPC

	require.NoError(t, cfg.Validate())
	require.NotNil(t, cfg.Clock)
	require.Equal(t, defaultTimeSkew, cfg.AuthTimeSkew)
	require.Equal(t, defaultPresignTTL, cfg.PresignTTL)
	require.Equal(t, defaultServiceabilityRefreshInterval, cfg.ServiceabilityRefreshInterval)
	require.Equal(t, defaultShutdownTimeout, cfg.ShutdownTimeout)
	require.Equal(t, int64(defaultMaxBodySize), cfg.MaxBodySize)
}

func TestTelemetry_StateIngest_Config_Validate_DoesNotOverrideProvidedValues(t *testing.T) {
	t.Parallel()

	okPresign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid"}, nil
		},
	}
	okRPC := mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	}

	fake := clockwork.NewFakeClock()
	cfg := Config{
		Presign:                       okPresign,
		BucketName:                    "bucket-1",
		BucketPathPrefix:              "pfx",
		ServiceabilityRPC:             okRPC,
		Clock:                         fake,
		AuthTimeSkew:                  9 * time.Minute,
		PresignTTL:                    2 * time.Minute,
		ServiceabilityRefreshInterval: 3 * time.Minute,
		ShutdownTimeout:               4 * time.Second,
		MaxBodySize:                   1234,
	}

	require.NoError(t, cfg.Validate())
	require.Same(t, fake, cfg.Clock)
	require.Equal(t, 9*time.Minute, cfg.AuthTimeSkew)
	require.Equal(t, 2*time.Minute, cfg.PresignTTL)
	require.Equal(t, 3*time.Minute, cfg.ServiceabilityRefreshInterval)
	require.Equal(t, 4*time.Second, cfg.ShutdownTimeout)
	require.Equal(t, int64(1234), cfg.MaxBodySize)
}

func TestTelemetry_StateIngest_Config_Validate_ZeroOrNegativeOptionalFieldsGetDefaults(t *testing.T) {
	t.Parallel()

	okPresign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid"}, nil
		},
	}
	okRPC := mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	}

	cfg := Config{
		Presign:           okPresign,
		BucketName:        "bucket-1",
		ServiceabilityRPC: okRPC,

		AuthTimeSkew:                  0,
		PresignTTL:                    -1,
		ServiceabilityRefreshInterval: 0,
		ShutdownTimeout:               -1,
		MaxBodySize:                   0,
	}

	require.NoError(t, cfg.Validate())
	require.Equal(t, defaultTimeSkew, cfg.AuthTimeSkew)
	require.Equal(t, defaultPresignTTL, cfg.PresignTTL)
	require.Equal(t, defaultServiceabilityRefreshInterval, cfg.ServiceabilityRefreshInterval)
	require.Equal(t, defaultShutdownTimeout, cfg.ShutdownTimeout)
	require.Equal(t, int64(defaultMaxBodySize), cfg.MaxBodySize)
}

func TestTelemetry_StateIngest_Config_Validate_AllowsEmptyBucketPathPrefix(t *testing.T) {
	t.Parallel()

	okPresign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid"}, nil
		},
	}
	okRPC := mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	}

	cfg := Config{
		Presign:           okPresign,
		BucketName:        "bucket-1",
		BucketPathPrefix:  "",
		ServiceabilityRPC: okRPC,
	}
	require.NoError(t, cfg.Validate())
}

func TestTelemetry_StateIngest_Config_Validate_Idempotent(t *testing.T) {
	t.Parallel()

	okPresign := mockPresignClient{
		PresignPutObjectFunc: func(ctx context.Context, input *s3.PutObjectInput, opts ...func(*s3.PresignOptions)) (*awssigner.PresignedHTTPRequest, error) {
			return &awssigner.PresignedHTTPRequest{URL: "https://example.invalid"}, nil
		},
	}
	okRPC := mockServiceabilityRPC{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return &serviceability.ProgramData{}, nil
		},
	}

	fakeClock := clockwork.NewFakeClock()

	cfg := Config{
		Presign:                       okPresign,
		BucketName:                    "bucket-1",
		BucketPathPrefix:              "prefix",
		ServiceabilityRPC:             okRPC,
		Clock:                         fakeClock,
		AuthTimeSkew:                  7 * time.Minute,
		PresignTTL:                    9 * time.Minute,
		ServiceabilityRefreshInterval: 11 * time.Minute,
		ShutdownTimeout:               13 * time.Second,
		MaxBodySize:                   42,
	}

	require.NoError(t, cfg.Validate())

	// Snapshot values after first validation
	clock1 := cfg.Clock
	authSkew1 := cfg.AuthTimeSkew
	presignTTL1 := cfg.PresignTTL
	refresh1 := cfg.ServiceabilityRefreshInterval
	shutdown1 := cfg.ShutdownTimeout
	maxBody1 := cfg.MaxBodySize

	require.NoError(t, cfg.Validate())

	// Ensure nothing changed
	require.Same(t, clock1, cfg.Clock)
	require.Equal(t, authSkew1, cfg.AuthTimeSkew)
	require.Equal(t, presignTTL1, cfg.PresignTTL)
	require.Equal(t, refresh1, cfg.ServiceabilityRefreshInterval)
	require.Equal(t, shutdown1, cfg.ShutdownTimeout)
	require.Equal(t, maxBody1, cfg.MaxBodySize)
}
