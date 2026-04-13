package data_test

import (
	"context"
	"testing"
	"time"

	"github.com/gagliardetto/solana-go"
	data "github.com/malbeclabs/doublezero/controlplane/telemetry/internal/data/device"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
)

func newTestProviderForAgentVersions(
	t *testing.T,
	programData *serviceability.ProgramData,
	headerFn func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamplesHeader, error),
	epochFn func(ctx context.Context, target time.Time) (uint64, error),
) data.Provider {
	t.Helper()

	p, err := data.NewProvider(&data.ProviderConfig{
		Logger: logger,
		ServiceabilityClient: &mockServiceabilityClient{
			GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
				return programData, nil
			},
		},
		TelemetryClient: &mockTelemetryClient{
			GetDeviceLatencySamplesFunc: func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamples, error) {
				return nil, telemetry.ErrAccountNotFound
			},
			GetDeviceLatencySamplesHeaderFunc: headerFn,
		},
		EpochFinder: &mockEpochFinder{
			ApproximateAtTimeFunc: epochFn,
		},
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return p
}

func makeProgramData(deviceA, deviceZ solana.PublicKey, linkPK, contributorPK solana.PublicKey) *serviceability.ProgramData {
	return &serviceability.ProgramData{
		Devices: []serviceability.Device{
			{PubKey: [32]byte(deviceA.Bytes()), Code: "dev-a"},
			{PubKey: [32]byte(deviceZ.Bytes()), Code: "dev-z"},
		},
		Links: []serviceability.Link{
			{
				PubKey:            [32]byte(linkPK.Bytes()),
				SideAPubKey:       [32]uint8(deviceA.Bytes()),
				SideZPubKey:       [32]uint8(deviceZ.Bytes()),
				ContributorPubKey: [32]uint8(contributorPK.Bytes()),
				Code:              "link-1",
			},
		},
		Contributors: []serviceability.Contributor{
			{PubKey: [32]byte(contributorPK.Bytes()), Code: "contrib-1"},
		},
	}
}

func makeHeader(epoch uint64, version, commit string, sampleCount uint32, startMicros, intervalMicros uint64) *telemetry.DeviceLatencySamplesHeader {
	var av [16]uint8
	copy(av[:], version)
	var ac [8]uint8
	copy(ac[:], commit)
	return &telemetry.DeviceLatencySamplesHeader{
		AccountType:                  telemetry.AccountTypeDeviceLatencySamples,
		Epoch:                        epoch,
		NextSampleIndex:              sampleCount,
		AgentVersion:                 av,
		AgentCommit:                  ac,
		StartTimestampMicroseconds:   startMicros,
		SamplingIntervalMicroseconds: intervalMicros,
	}
}

func TestGetAgentVersions_ReturnsVersions(t *testing.T) {
	deviceA := solana.NewWallet().PublicKey()
	deviceZ := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	contributorPK := solana.NewWallet().PublicKey()

	now := time.Now().UTC()
	startMicros := uint64(now.Add(-1 * time.Hour).UnixMicro())

	pd := makeProgramData(deviceA, deviceZ, linkPK, contributorPK)

	p := newTestProviderForAgentVersions(t, pd,
		func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamplesHeader, error) {
			return makeHeader(100, "v1.2.3", "abc1234", 100, startMicros, 1_000_000), nil
		},
		func(ctx context.Context, target time.Time) (uint64, error) {
			return 100, nil
		},
	)

	versions, err := p.GetAgentVersions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 2 {
		t.Fatalf("expected 2 versions (forward + reverse circuit), got %d", len(versions))
	}

	for _, v := range versions {
		if v.Version != "v1.2.3" {
			t.Errorf("expected version v1.2.3, got %q", v.Version)
		}
		if v.Commit != "abc1234" {
			t.Errorf("expected commit abc1234, got %q", v.Commit)
		}
	}
}

func TestGetAgentVersions_SkipsAccountNotFound(t *testing.T) {
	deviceA := solana.NewWallet().PublicKey()
	deviceZ := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	contributorPK := solana.NewWallet().PublicKey()

	pd := makeProgramData(deviceA, deviceZ, linkPK, contributorPK)

	p := newTestProviderForAgentVersions(t, pd,
		func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamplesHeader, error) {
			return nil, telemetry.ErrAccountNotFound
		},
		func(ctx context.Context, target time.Time) (uint64, error) {
			return 100, nil
		},
	)

	versions, err := p.GetAgentVersions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 0 {
		t.Fatalf("expected 0 versions, got %d", len(versions))
	}
}

func TestGetAgentVersions_SkipsZeroSampleIndex(t *testing.T) {
	deviceA := solana.NewWallet().PublicKey()
	deviceZ := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	contributorPK := solana.NewWallet().PublicKey()

	pd := makeProgramData(deviceA, deviceZ, linkPK, contributorPK)

	p := newTestProviderForAgentVersions(t, pd,
		func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamplesHeader, error) {
			return makeHeader(100, "v1.0.0", "deadbeef", 0, 0, 0), nil
		},
		func(ctx context.Context, target time.Time) (uint64, error) {
			return 100, nil
		},
	)

	versions, err := p.GetAgentVersions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 0 {
		t.Fatalf("expected 0 versions for zero sample index, got %d", len(versions))
	}
}

func TestGetAgentVersions_SkipsStaleEntries(t *testing.T) {
	deviceA := solana.NewWallet().PublicKey()
	deviceZ := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	contributorPK := solana.NewWallet().PublicKey()

	// Last sample time is >24h ago
	startMicros := uint64(time.Now().UTC().Add(-48 * time.Hour).UnixMicro())

	pd := makeProgramData(deviceA, deviceZ, linkPK, contributorPK)

	p := newTestProviderForAgentVersions(t, pd,
		func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamplesHeader, error) {
			return makeHeader(100, "v1.0.0", "abc", 10, startMicros, 1_000_000), nil
		},
		func(ctx context.Context, target time.Time) (uint64, error) {
			return 100, nil
		},
	)

	versions, err := p.GetAgentVersions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 0 {
		t.Fatalf("expected 0 stale versions, got %d", len(versions))
	}
}

func TestGetAgentVersions_FallsBackToPreviousEpoch(t *testing.T) {
	deviceA := solana.NewWallet().PublicKey()
	deviceZ := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	contributorPK := solana.NewWallet().PublicKey()

	now := time.Now().UTC()
	startMicros := uint64(now.Add(-1 * time.Hour).UnixMicro())

	pd := makeProgramData(deviceA, deviceZ, linkPK, contributorPK)

	p := newTestProviderForAgentVersions(t, pd,
		func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamplesHeader, error) {
			if epoch == 100 {
				return nil, telemetry.ErrAccountNotFound
			}
			// epoch 99
			return makeHeader(99, "v0.9.0", "old1234", 50, startMicros, 1_000_000), nil
		},
		func(ctx context.Context, target time.Time) (uint64, error) {
			return 100, nil
		},
	)

	versions, err := p.GetAgentVersions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) == 0 {
		t.Fatal("expected versions from fallback epoch, got 0")
	}

	for _, v := range versions {
		if v.Version != "v0.9.0" {
			t.Errorf("expected version v0.9.0 from previous epoch, got %q", v.Version)
		}
	}
}

func TestGetAgentVersions_EmptyCircuits(t *testing.T) {
	pd := &serviceability.ProgramData{}

	p := newTestProviderForAgentVersions(t, pd,
		func(ctx context.Context, origin, target, link solana.PublicKey, epoch uint64) (*telemetry.DeviceLatencySamplesHeader, error) {
			return nil, telemetry.ErrAccountNotFound
		},
		func(ctx context.Context, target time.Time) (uint64, error) {
			return 100, nil
		},
	)

	versions, err := p.GetAgentVersions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(versions) != 0 {
		t.Fatalf("expected 0 versions for empty circuits, got %d", len(versions))
	}
}
