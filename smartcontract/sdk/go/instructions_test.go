package dzsdk

import (
	"bytes"
	"testing"

	"github.com/gagliardetto/solana-go"
)

func TestSerializeInitializeDzLatencySamples(t *testing.T) {
	deviceAPk := solana.NewWallet().PublicKey()
	deviceZPk := solana.NewWallet().PublicKey()
	linkPk := solana.NewWallet().PublicKey()

	args := &InitializeDzLatencySamplesArgs{
		DeviceAPk:                    deviceAPk,
		DeviceZPk:                    deviceZPk,
		LinkPk:                       linkPk,
		Epoch:                        100,
		SamplingIntervalMicroseconds: 1000000, // 1 second
	}

	data, err := SerializeInitializeDzLatencySamples(args)
	if err != nil {
		t.Fatalf("Failed to serialize InitializeDzLatencySamples: %v", err)
	}

	// Verify discriminator
	if data[0] != uint8(InitializeDzLatencySamplesInstruction) {
		t.Errorf("Expected discriminator %d, got %d", InitializeDzLatencySamplesInstruction, data[0])
	}

	// Verify minimum length (discriminator + 3 pubkeys + 2 uint64s)
	expectedMinLength := 1 + 32*3 + 8*2
	if len(data) < expectedMinLength {
		t.Errorf("Expected data length >= %d, got %d", expectedMinLength, len(data))
	}

	// Verify pubkeys are in the correct positions
	if !bytes.Equal(data[1:33], deviceAPk[:]) {
		t.Error("DeviceAPk not serialized correctly")
	}
	if !bytes.Equal(data[33:65], deviceZPk[:]) {
		t.Error("DeviceZPk not serialized correctly")
	}
	if !bytes.Equal(data[65:97], linkPk[:]) {
		t.Error("LinkPk not serialized correctly")
	}
}

func TestSerializeWriteDzLatencySamples(t *testing.T) {
	samples := []uint32{100, 200, 300, 400, 500}
	args := &WriteDzLatencySamplesArgs{
		StartTimestampMicroseconds: 1234567890,
		Samples:                    samples,
	}

	data, err := SerializeWriteDzLatencySamples(args)
	if err != nil {
		t.Fatalf("Failed to serialize WriteDzLatencySamples: %v", err)
	}

	// Verify discriminator
	if data[0] != uint8(WriteDzLatencySamplesInstruction) {
		t.Errorf("Expected discriminator %d, got %d", WriteDzLatencySamplesInstruction, data[0])
	}

	// Verify data is not empty beyond discriminator
	if len(data) <= 1 {
		t.Error("Serialized data is too short")
	}
}

func TestSerializeWriteDzLatencySamplesEmpty(t *testing.T) {
	// Test with empty samples
	args := &WriteDzLatencySamplesArgs{
		StartTimestampMicroseconds: 1234567890,
		Samples:                    []uint32{},
	}

	data, err := SerializeWriteDzLatencySamples(args)
	if err != nil {
		t.Fatalf("Failed to serialize WriteDzLatencySamples with empty samples: %v", err)
	}

	// Should still have discriminator
	if data[0] != uint8(WriteDzLatencySamplesInstruction) {
		t.Errorf("Expected discriminator %d, got %d", WriteDzLatencySamplesInstruction, data[0])
	}
}

func TestSerializeWriteDzLatencySamplesLarge(t *testing.T) {
	samples := make([]uint32, MAX_SAMPLES)
	for i := range samples {
		samples[i] = uint32(i * 100)
	}

	args := &WriteDzLatencySamplesArgs{
		StartTimestampMicroseconds: 9876543210,
		Samples:                    samples,
	}

	data, err := SerializeWriteDzLatencySamples(args)
	if err != nil {
		t.Fatalf("Failed to serialize WriteDzLatencySamples with max samples: %v", err)
	}

	// Verify discriminator
	if data[0] != uint8(WriteDzLatencySamplesInstruction) {
		t.Errorf("Expected discriminator %d, got %d", WriteDzLatencySamplesInstruction, data[0])
	}

	// Verify reasonable size
	if len(data) > DZ_LATENCY_SAMPLES_MAX_SIZE {
		t.Errorf("Serialized data exceeds max size: %d > %d", len(data), DZ_LATENCY_SAMPLES_MAX_SIZE)
	}
}
