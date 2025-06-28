package telemetry_test

import (
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/telemetry"
	"github.com/near/borsh-go"
	"github.com/stretchr/testify/require"
)

func TestSDK_Telemetry_WriteDeviceLatencySamples_HappyPath(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	agentPK := solana.NewWallet().PublicKey()
	originPK := solana.NewWallet().PublicKey()
	targetPK := solana.NewWallet().PublicKey()
	linkPK := solana.NewWallet().PublicKey()
	epoch := uint64(123)
	timestamp := uint64(1_600_000_000)
	samples := []uint32{1, 2, 3, 4}

	config := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    agentPK,
		OriginDevicePK:             originPK,
		TargetDevicePK:             targetPK,
		LinkPK:                     linkPK,
		Epoch:                      epoch,
		StartTimestampMicroseconds: timestamp,
		Samples:                    samples,
	}

	ix, err := telemetry.BuildWriteDeviceLatencySamplesInstruction(programID, config)
	require.NoError(t, err)
	require.NotNil(t, ix)

	require.Equal(t, programID, ix.ProgramID())
	accounts := ix.Accounts()
	require.Len(t, accounts, 3)

	require.Equal(t, agentPK, accounts[1].PublicKey)
	require.True(t, accounts[1].IsSigner)
	require.False(t, accounts[1].IsWritable)

	require.Equal(t, solana.SystemProgramID, accounts[2].PublicKey)
	require.False(t, accounts[2].IsSigner)
	require.False(t, accounts[2].IsWritable)

	data, err := ix.Data()
	require.NoError(t, err)
	require.Greater(t, len(data), 0)
	require.Equal(t, uint8(telemetry.WriteDeviceLatencySamplesInstructionIndex), data[0])
}

func TestSDK_Telemetry_WriteDeviceLatencySamples_MissingFields(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	base := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    solana.NewWallet().PublicKey(),
		OriginDevicePK:             solana.NewWallet().PublicKey(),
		TargetDevicePK:             solana.NewWallet().PublicKey(),
		LinkPK:                     solana.NewWallet().PublicKey(),
		Epoch:                      123,
		StartTimestampMicroseconds: 1_600_000_000,
		Samples:                    []uint32{10, 20},
	}

	tests := []struct {
		name        string
		mutate      func(*telemetry.WriteDeviceLatencySamplesInstructionConfig)
		expectError string
	}{
		{
			name:        "missing_agent_pk",
			mutate:      func(c *telemetry.WriteDeviceLatencySamplesInstructionConfig) { c.AgentPK = solana.PublicKey{} },
			expectError: "agent public key is required",
		},
		{
			name:        "missing_origin_device_pk",
			mutate:      func(c *telemetry.WriteDeviceLatencySamplesInstructionConfig) { c.OriginDevicePK = solana.PublicKey{} },
			expectError: "origin device public key is required",
		},
		{
			name:        "missing_target_device_pk",
			mutate:      func(c *telemetry.WriteDeviceLatencySamplesInstructionConfig) { c.TargetDevicePK = solana.PublicKey{} },
			expectError: "target device public key is required",
		},
		{
			name:        "missing_link_pk",
			mutate:      func(c *telemetry.WriteDeviceLatencySamplesInstructionConfig) { c.LinkPK = solana.PublicKey{} },
			expectError: "link public key is required",
		},
		{
			name:        "missing_epoch",
			mutate:      func(c *telemetry.WriteDeviceLatencySamplesInstructionConfig) { c.Epoch = 0 },
			expectError: "epoch is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := base
			tt.mutate(&config)

			ix, err := telemetry.BuildWriteDeviceLatencySamplesInstruction(programID, config)
			require.ErrorContains(t, err, tt.expectError)
			require.Nil(t, ix)
		})
	}
}

func TestSDK_Telemetry_WriteDeviceLatencySamples_BorshEncoding(t *testing.T) {
	t.Parallel()

	programID := solana.NewWallet().PublicKey()
	timestamp := uint64(1_650_000_000)
	samples := []uint32{100, 200, 300}

	config := telemetry.WriteDeviceLatencySamplesInstructionConfig{
		AgentPK:                    solana.NewWallet().PublicKey(),
		OriginDevicePK:             solana.NewWallet().PublicKey(),
		TargetDevicePK:             solana.NewWallet().PublicKey(),
		LinkPK:                     solana.NewWallet().PublicKey(),
		Epoch:                      555,
		StartTimestampMicroseconds: timestamp,
		Samples:                    samples,
	}

	ix, err := telemetry.BuildWriteDeviceLatencySamplesInstruction(programID, config)
	require.NoError(t, err)

	var decoded struct {
		Discriminator              uint8
		StartTimestampMicroseconds uint64
		Samples                    []uint32
	}

	data, err := ix.Data()
	require.NoError(t, err)

	err = borsh.Deserialize(&decoded, data)
	require.NoError(t, err)

	require.Equal(t, uint8(telemetry.WriteDeviceLatencySamplesInstructionIndex), decoded.Discriminator)
	require.Equal(t, timestamp, decoded.StartTimestampMicroseconds)
	require.Equal(t, samples, decoded.Samples)
}
