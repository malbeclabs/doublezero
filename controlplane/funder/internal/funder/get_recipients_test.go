package funder

import (
	"context"
	"errors"
	"testing"

	"github.com/gagliardetto/solana-go"
	serviceability "github.com/malbeclabs/doublezero/sdk/serviceability/go"
	"github.com/stretchr/testify/assert"
)

// MockServiceabilityClient implements the minimal interface for testing
type MockServiceabilityClient struct {
	GetProgramDataFunc func(ctx context.Context) (*serviceability.ProgramData, error)
}

func (m *MockServiceabilityClient) GetProgramData(ctx context.Context) (*serviceability.ProgramData, error) {
	return m.GetProgramDataFunc(ctx)
}

func (m *MockServiceabilityClient) ProgramID() solana.PublicKey {
	return solana.PublicKey{}
}

func TestGetRecipients_Success(t *testing.T) {
	mockData := &serviceability.ProgramData{
		Devices: []serviceability.Device{
			{
				PubKey:                 [32]byte{1},
				MetricsPublisherPubKey: [32]byte{2},
			},
		},
		MulticastGroups: []serviceability.MulticastGroup{
			{
				PubKey: [32]byte{3},
				Owner:  [32]byte{4},
			},
		},
	}
	client := &MockServiceabilityClient{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return mockData, nil
		},
	}
	recipients := []Recipient{}
	pk := [32]byte{5}
	internetLatencyCollectorPK := solana.PublicKeyFromBytes(pk[:])

	result, err := GetRecipients(context.Background(), client, recipients, internetLatencyCollectorPK)
	assert.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Equal(t, "device-"+solana.PublicKeyFromBytes(mockData.Devices[0].PubKey[:]).String(), result[0].Name)
	assert.Equal(t, solana.PublicKeyFromBytes(mockData.Devices[0].MetricsPublisherPubKey[:]), result[0].PubKey)
	assert.Equal(t, "mcastgroup-"+solana.PublicKeyFromBytes(mockData.MulticastGroups[0].PubKey[:]).String(), result[1].Name)
	assert.Equal(t, solana.PublicKeyFromBytes(mockData.MulticastGroups[0].Owner[:]), result[1].PubKey)
	assert.Equal(t, "internet-latency-collector", result[2].Name)
	assert.Equal(t, internetLatencyCollectorPK, result[2].PubKey)
}

func TestGetRecipients_Error(t *testing.T) {
	client := &MockServiceabilityClient{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return nil, errors.New("fail")
		},
	}
	recipients := []Recipient{}
	pk := [32]byte{5}
	internetLatencyCollectorPK := solana.PublicKeyFromBytes(pk[:])

	result, err := GetRecipients(context.Background(), client, recipients, internetLatencyCollectorPK)
	assert.Error(t, err)
	assert.Nil(t, result)
}
