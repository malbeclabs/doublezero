package funder

import (
	"context"
	"errors"
	"testing"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
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
		Contributors: []serviceability.Contributor{
			{
				PubKey: [32]byte{6},
				Owner:  [32]byte{7},
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
	assert.Len(t, result, 4)
	assert.Equal(t, "device-"+solana.PublicKeyFromBytes(mockData.Devices[0].PubKey[:]).String(), result[0].Name)
	assert.Equal(t, solana.PublicKeyFromBytes(mockData.Devices[0].MetricsPublisherPubKey[:]), result[0].PubKey)
	assert.Equal(t, "contributor-"+solana.PublicKeyFromBytes(mockData.Contributors[0].PubKey[:]).String(), result[1].Name)
	assert.Equal(t, solana.PublicKeyFromBytes(mockData.Contributors[0].Owner[:]), result[1].PubKey)
	assert.Equal(t, "mcastgroup-"+solana.PublicKeyFromBytes(mockData.MulticastGroups[0].PubKey[:]).String(), result[2].Name)
	assert.Equal(t, solana.PublicKeyFromBytes(mockData.MulticastGroups[0].Owner[:]), result[2].PubKey)
	assert.Equal(t, "internet-latency-collector", result[3].Name)
	assert.Equal(t, internetLatencyCollectorPK, result[3].PubKey)
}

func TestGetRecipients_Dedup(t *testing.T) {
	sharedOwner := [32]byte{7}
	mockData := &serviceability.ProgramData{
		Contributors: []serviceability.Contributor{
			{
				PubKey: [32]byte{6},
				Owner:  sharedOwner,
			},
		},
		MulticastGroups: []serviceability.MulticastGroup{
			{
				PubKey: [32]byte{3},
				Owner:  sharedOwner, // same key as contributor owner
			},
		},
	}
	client := &MockServiceabilityClient{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return mockData, nil
		},
	}
	pk := [32]byte{5}
	internetLatencyCollectorPK := solana.PublicKeyFromBytes(pk[:])

	result, err := GetRecipients(context.Background(), client, nil, internetLatencyCollectorPK)
	assert.NoError(t, err)
	assert.Len(t, result, 2) // contributor + internet-latency-collector, mcastgroup deduped
	assert.Equal(t, "contributor-"+solana.PublicKeyFromBytes(mockData.Contributors[0].PubKey[:]).String(), result[0].Name)
	assert.Equal(t, solana.PublicKeyFromBytes(sharedOwner[:]), result[0].PubKey)
	assert.Equal(t, "internet-latency-collector", result[1].Name)
}

func TestGetRecipients_DedupWithPreloaded(t *testing.T) {
	sharedOwner := [32]byte{7}
	mockData := &serviceability.ProgramData{
		Contributors: []serviceability.Contributor{
			{
				PubKey: [32]byte{6},
				Owner:  sharedOwner,
			},
		},
	}
	client := &MockServiceabilityClient{
		GetProgramDataFunc: func(ctx context.Context) (*serviceability.ProgramData, error) {
			return mockData, nil
		},
	}
	pk := [32]byte{5}
	internetLatencyCollectorPK := solana.PublicKeyFromBytes(pk[:])

	// Pre-load a recipient with the same pubkey as the contributor owner.
	preloaded := []Recipient{
		NewRecipient("preloaded", solana.PublicKeyFromBytes(sharedOwner[:])),
	}

	result, err := GetRecipients(context.Background(), client, preloaded, internetLatencyCollectorPK)
	assert.NoError(t, err)
	assert.Len(t, result, 2) // preloaded + internet-latency-collector, contributor deduped
	assert.Equal(t, "preloaded", result[0].Name)
	assert.Equal(t, "internet-latency-collector", result[1].Name)
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
