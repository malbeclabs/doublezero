package telemetry_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/near/borsh-go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_InitializeDeviceLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	agentPK := solana.NewWallet().PublicKey()
	originDevicePK := solana.NewWallet().PublicKey()
	targetDevicePK := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	epoch := uint64(42)
	interval := uint64(100_000)

	config := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		AgentPK:                      agentPK,
		OriginDevicePK:               originDevicePK,
		TargetDevicePK:               targetDevicePK,
		LinkPK:                       linkPK,
		Epoch:                        epoch,
		SamplingIntervalMicroseconds: interval,
	}

	ix, err := telemetry.BuildInitializeDeviceLatencySamplesInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, ix)

	require.Equal(t, programID, ix.ProgramID(), "program ID should match")

	accounts := ix.Accounts()
	require.Len(t, accounts, 5)

	require.Equal(t, agentPK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsSigner)
	require.True(t, accounts[1].IsWritable)

	data, err := ix.Data()
	require.NoError(t, err)
	require.Greater(t, len(data), 0, "instruction data should not be empty")
	require.Equal(t, uint8(telemetry.InitializeDeviceLatencySamplesInstructionIndex), data[0], "discriminator mismatch")
}
func TestSDK_Telemetry_InitializeDeviceLatencySamples_MissingFields(t *testing.T) {
	t.Parallel()

	base := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		AgentPK:                      solana.NewWallet().PublicKey(),
		OriginDevicePK:               solana.NewWallet().PublicKey(),
		TargetDevicePK:               solana.NewWallet().PublicKey(),
		LinkPK:                       solana.NewWallet().PublicKey(),
		Epoch:                        42,
		SamplingIntervalMicroseconds: 100_000,
	}

	tests := []struct {
		name        string
		mutate      func(c *telemetry.InitializeDeviceLatencySamplesInstructionConfig)
		expectError string
	}{
		{
			name:        "missing_agent_pk",
			mutate:      func(c *telemetry.InitializeDeviceLatencySamplesInstructionConfig) { c.AgentPK = solana.PublicKey{} },
			expectError: "agent public key is required",
		},
		{
			name: "missing_origin_device_pk",
			mutate: func(c *telemetry.InitializeDeviceLatencySamplesInstructionConfig) {
				c.OriginDevicePK = solana.PublicKey{}
			},
			expectError: "origin device public key is required",
		},
		{
			name: "missing_target_device_pk",
			mutate: func(c *telemetry.InitializeDeviceLatencySamplesInstructionConfig) {
				c.TargetDevicePK = solana.PublicKey{}
			},
			expectError: "target device public key is required",
		},
		{
			name:        "missing_link_pk",
			mutate:      func(c *telemetry.InitializeDeviceLatencySamplesInstructionConfig) { c.LinkPK = solana.PublicKey{} },
			expectError: "link public key is required",
		},
		{
			name:        "missing_epoch",
			mutate:      func(c *telemetry.InitializeDeviceLatencySamplesInstructionConfig) { c.Epoch = 0 },
			expectError: "epoch is required",
		},
		{
			name:        "missing_sampling_interval",
			mutate:      func(c *telemetry.InitializeDeviceLatencySamplesInstructionConfig) { c.SamplingIntervalMicroseconds = 0 },
			expectError: "sampling interval microseconds is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.mutate(&config)

			programID := solana.NewWallet().PublicKey()
			ix, err := telemetry.BuildInitializeDeviceLatencySamplesInstruction(programID, config)
			require.ErrorContains(t, err, tt.expectError)
			require.Nil(t, ix)
		})
	}
}

func TestSDK_Telemetry_InitializeDeviceLatencySamples_BorshEncoding(t *testing.T) {
	t.Parallel()

	config := telemetry.InitializeDeviceLatencySamplesInstructionConfig{
		AgentPK:                      solana.NewWallet().PublicKey(),
		OriginDevicePK:               solana.NewWallet().PublicKey(),
		TargetDevicePK:               solana.NewWallet().PublicKey(),
		LinkPK:                       solana.NewWallet().PublicKey(),
		Epoch:                        99,
		SamplingIntervalMicroseconds: 250_000,
	}

	programID := solana.NewWallet().PublicKey()
	ix, err := telemetry.BuildInitializeDeviceLatencySamplesInstruction(programID, config)
	require.NoError(t, err)

	var decoded struct {
		Discriminator                uint8
		Epoch                        uint64
		SamplingIntervalMicroseconds uint64
	}

	data, err := ix.Data()
	require.NoError(t, err)

	err = borsh.Deserialize(&decoded, data)
	require.NoError(t, err)

	require.Equal(t, uint8(telemetry.InitializeDeviceLatencySamplesInstructionIndex), decoded.Discriminator)
	require.Equal(t, config.Epoch, decoded.Epoch)
	require.Equal(t, config.SamplingIntervalMicroseconds, decoded.SamplingIntervalMicroseconds)
}
