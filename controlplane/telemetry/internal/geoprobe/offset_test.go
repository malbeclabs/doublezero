package geoprobe

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLocationOffset_MarshalUnmarshal(t *testing.T) {
	t.Parallel()

	// Create a test offset with Amsterdam coordinates (from RFC16)
	offset := &LocationOffset{
		Signature:       [64]byte{1, 2, 3, 4, 5},
		AuthorityPubkey: [32]byte{10, 11, 12, 13, 14},
		SenderPubkey:    [32]byte{20, 21, 22, 23, 24},
		MeasurementSlot: 123456789,
		MeasuredRttNs:   800000,  // 0.8ms
		Lat:             52.3676, // Amsterdam
		Lng:             4.9041,
		RttNs:           12500000, // 12.5ms
		NumReferences:   0,
		References:      nil,
	}

	// Marshal to bytes
	data, err := offset.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Unmarshal back
	decoded := &LocationOffset{}
	err = decoded.Unmarshal(data)
	require.NoError(t, err)

	// Verify all fields match
	require.Equal(t, offset.Signature, decoded.Signature)
	require.Equal(t, offset.AuthorityPubkey, decoded.AuthorityPubkey)
	require.Equal(t, offset.SenderPubkey, decoded.SenderPubkey)
	require.Equal(t, offset.MeasurementSlot, decoded.MeasurementSlot)
	require.Equal(t, offset.Lat, decoded.Lat)
	require.Equal(t, offset.Lng, decoded.Lng)
	require.Equal(t, offset.MeasuredRttNs, decoded.MeasuredRttNs)
	require.Equal(t, offset.RttNs, decoded.RttNs)
	require.Equal(t, offset.NumReferences, decoded.NumReferences)
	require.Len(t, decoded.References, 0)
}

func TestLocationOffset_WithReferences(t *testing.T) {
	t.Parallel()

	// Create a DZD offset (no references)
	dzdOffset := &LocationOffset{
		Signature:       [64]byte{1, 2, 3},
		AuthorityPubkey: [32]byte{10, 11, 12},
		MeasurementSlot: 100,
		MeasuredRttNs:   800000,
		Lat:             50.1109, // Frankfurt
		Lng:             8.6821,
		RttNs:           800000,
		NumReferences:   0,
		References:      nil,
	}

	// Create a Probe offset that references the DZD offset
	probeOffset := &LocationOffset{
		Signature:       [64]byte{4, 5, 6},
		AuthorityPubkey: [32]byte{20, 21, 22},
		MeasurementSlot: 101,
		MeasuredRttNs:   12500000, // 12.5ms probe-to-target
		Lat:             50.1109,  // Copied from DZD
		Lng:             8.6821,
		RttNs:           13300000, // 800000 + 12500000
		NumReferences:   1,
		References:      []LocationOffset{*dzdOffset},
	}

	// Marshal the probe offset (should include reference)
	data, err := probeOffset.Marshal()
	require.NoError(t, err)
	require.NotEmpty(t, data)

	// Unmarshal back
	decoded := &LocationOffset{}
	err = decoded.Unmarshal(data)
	require.NoError(t, err)

	// Verify top-level fields
	require.Equal(t, probeOffset.Signature, decoded.Signature)
	require.Equal(t, probeOffset.AuthorityPubkey, decoded.AuthorityPubkey)
	require.Equal(t, probeOffset.SenderPubkey, decoded.SenderPubkey)
	require.Equal(t, probeOffset.MeasurementSlot, decoded.MeasurementSlot)
	require.Equal(t, probeOffset.Lat, decoded.Lat)
	require.Equal(t, probeOffset.Lng, decoded.Lng)
	require.Equal(t, probeOffset.MeasuredRttNs, decoded.MeasuredRttNs)
	require.Equal(t, probeOffset.RttNs, decoded.RttNs)
	require.Equal(t, probeOffset.NumReferences, decoded.NumReferences)

	// Verify reference chain
	require.Len(t, decoded.References, 1)
	require.Equal(t, dzdOffset.Signature, decoded.References[0].Signature)
	require.Equal(t, dzdOffset.AuthorityPubkey, decoded.References[0].AuthorityPubkey)
	require.Equal(t, dzdOffset.SenderPubkey, decoded.References[0].SenderPubkey)
	require.Equal(t, dzdOffset.MeasurementSlot, decoded.References[0].MeasurementSlot)
	require.Equal(t, dzdOffset.Lat, decoded.References[0].Lat)
	require.Equal(t, dzdOffset.Lng, decoded.References[0].Lng)
	require.Equal(t, dzdOffset.NumReferences, decoded.References[0].NumReferences)
}

func TestLocationOffset_EmptyReferences(t *testing.T) {
	t.Parallel()

	// DZD-generated offset with no references
	offset := &LocationOffset{
		Signature:       [64]byte{},
		AuthorityPubkey: [32]byte{},
		MeasurementSlot: 1,
		MeasuredRttNs:   1000,
		Lat:             0.0,
		Lng:             0.0,
		RttNs:           1000,
		NumReferences:   0,
		References:      nil,
	}

	// Marshal
	data, err := offset.Marshal()
	require.NoError(t, err)

	// Unmarshal
	decoded := &LocationOffset{}
	err = decoded.Unmarshal(data)
	require.NoError(t, err)

	require.Equal(t, uint8(0), decoded.NumReferences)
	require.Empty(t, decoded.References)
}

func TestLocationOffset_GetSigningBytes(t *testing.T) {
	t.Parallel()

	offset := &LocationOffset{
		Signature:       [64]byte{99, 99, 99}, // Should be excluded from signing bytes
		AuthorityPubkey: [32]byte{1, 2, 3},
		MeasurementSlot: 42,
		MeasuredRttNs:   1000,
		Lat:             10.5,
		Lng:             20.5,
		RttNs:           2000,
		NumReferences:   0,
		References:      nil,
	}

	// Get signing bytes
	signingBytes, err := offset.GetSigningBytes()
	require.NoError(t, err)
	require.NotEmpty(t, signingBytes)

	// Signing bytes should not include the signature field
	// Marshal includes signature, GetSigningBytes does not
	fullBytes, err := offset.Marshal()
	require.NoError(t, err)

	// Signing bytes should be 64 bytes shorter (signature is 64 bytes)
	require.Equal(t, len(fullBytes)-64, len(signingBytes))
}

func TestLocationOffset_GetSigningBytes_WithReferences(t *testing.T) {
	t.Parallel()

	dzdOffset := &LocationOffset{
		Signature:       [64]byte{1},
		AuthorityPubkey: [32]byte{1},
		MeasurementSlot: 100,
		MeasuredRttNs:   1000,
		Lat:             1.0,
		Lng:             2.0,
		RttNs:           1000,
		NumReferences:   0,
		References:      nil,
	}

	probeOffset := &LocationOffset{
		Signature:       [64]byte{2},
		AuthorityPubkey: [32]byte{2},
		MeasurementSlot: 101,
		MeasuredRttNs:   2000,
		Lat:             1.0,
		Lng:             2.0,
		RttNs:           3000,
		NumReferences:   1,
		References:      []LocationOffset{*dzdOffset},
	}

	// Get signing bytes for probe offset (should include reference's full marshaled form)
	signingBytes, err := probeOffset.GetSigningBytes()
	require.NoError(t, err)
	require.NotEmpty(t, signingBytes)

	// The signing bytes should include the entire serialized reference
	dzdBytes, err := dzdOffset.Marshal()
	require.NoError(t, err)

	// Signing bytes should be at least as large as the reference
	require.Greater(t, len(signingBytes), len(dzdBytes))
}

func TestLocationOffset_UnmarshalError_TruncatedData(t *testing.T) {
	t.Parallel()

	offset := &LocationOffset{
		Signature:       [64]byte{1, 2, 3},
		AuthorityPubkey: [32]byte{4, 5, 6},
		MeasurementSlot: 123,
		MeasuredRttNs:   1000,
		Lat:             1.0,
		Lng:             2.0,
		RttNs:           2000,
		NumReferences:   0,
		References:      nil,
	}

	data, err := offset.Marshal()
	require.NoError(t, err)

	// Try to unmarshal truncated data
	truncated := data[:len(data)/2]
	decoded := &LocationOffset{}
	err = decoded.Unmarshal(truncated)
	require.Error(t, err)
}

func TestLocationOffset_Size(t *testing.T) {
	t.Parallel()

	// Minimal offset
	offset := &LocationOffset{
		Signature:       [64]byte{},
		AuthorityPubkey: [32]byte{},
		MeasurementSlot: 0,
		MeasuredRttNs:   0,
		Lat:             0.0,
		Lng:             0.0,
		RttNs:           0,
		NumReferences:   0,
		References:      nil,
	}

	size, err := offset.size()
	require.NoError(t, err)
	require.Greater(t, size, 0)

	// Size should be: 64 (sig) + 32 (authority) + 32 (sender) + 8 (slot) + 8 (lat) + 8 (lng) + 8 (measured) + 8 (rtt) + 1 (numref) = 169 bytes
	require.Equal(t, 169, size)
}

func TestLocationOffset_ReferenceDepthLimit(t *testing.T) {
	t.Parallel()

	// Create a chain that exceeds MaxReferenceDepth (2)
	current := &LocationOffset{
		Signature:       [64]byte{0},
		AuthorityPubkey: [32]byte{0},
		MeasurementSlot: 0,
		MeasuredRttNs:   100,
		Lat:             1.0,
		Lng:             2.0,
		RttNs:           100,
		NumReferences:   0,
		References:      nil,
	}

	// Build a chain of depth 3 (exceeding limit of 2)
	for i := 1; i <= 3; i++ {
		parent := &LocationOffset{
			Signature:       [64]byte{byte(i)},
			AuthorityPubkey: [32]byte{byte(i)},
			MeasurementSlot: uint64(i),
			MeasuredRttNs:   uint64((i + 1) * 100),
			Lat:             1.0,
			Lng:             2.0,
			RttNs:           uint64((i + 1) * 100),
			NumReferences:   1,
			References:      []LocationOffset{*current},
		}
		current = parent
	}

	// Marshal the deep chain
	data, err := current.Marshal()
	require.NoError(t, err)

	// Attempt to unmarshal should fail due to depth limit
	decoded := &LocationOffset{}
	err = decoded.Unmarshal(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "reference chain depth")
	require.Contains(t, err.Error(), "exceeds maximum")
}

func TestLocationOffset_TotalReferencesLimit(t *testing.T) {
	t.Parallel()

	// Create a wide tree structure that exceeds MaxTotalReferences (5)
	// Root with 3 children, each child has 1 child = 3 + 3 = 6 total refs

	// Leaf nodes
	leafOffsets := make([]LocationOffset, 3)
	for i := 0; i < 3; i++ {
		leafOffsets[i] = LocationOffset{
			Signature:       [64]byte{byte(i)},
			AuthorityPubkey: [32]byte{byte(i)},
			MeasurementSlot: uint64(i),
			MeasuredRttNs:   100,
			Lat:             1.0,
			Lng:             2.0,
			RttNs:           100,
			NumReferences:   0,
			References:      nil,
		}
	}

	// Middle nodes, each referencing 1 leaf
	midOffsets := make([]LocationOffset, 3)
	for i := 0; i < 3; i++ {
		midOffsets[i] = LocationOffset{
			Signature:       [64]byte{byte(100 + i)},
			AuthorityPubkey: [32]byte{byte(100 + i)},
			MeasurementSlot: uint64(100 + i),
			MeasuredRttNs:   200,
			Lat:             1.0,
			Lng:             2.0,
			RttNs:           300,
			NumReferences:   1,
			References:      []LocationOffset{leafOffsets[i]},
		}
	}

	// Root node referencing all 3 middle nodes (total refs = 3 + 3 = 6, exceeds limit of 5)
	rootOffset := &LocationOffset{
		Signature:       [64]byte{200},
		AuthorityPubkey: [32]byte{200},
		MeasurementSlot: 200,
		MeasuredRttNs:   300,
		Lat:             1.0,
		Lng:             2.0,
		RttNs:           600,
		NumReferences:   3,
		References:      midOffsets,
	}

	// Marshal the root
	data, err := rootOffset.Marshal()
	require.NoError(t, err)

	// Attempt to unmarshal should fail due to total reference count limit
	decoded := &LocationOffset{}
	err = decoded.Unmarshal(data)
	require.Error(t, err)
	require.Contains(t, err.Error(), "total reference count")
	require.Contains(t, err.Error(), "exceeds maximum")
}

func TestLocationOffset_ValidReferenceLimits(t *testing.T) {
	t.Parallel()

	// Create a chain at exactly the depth limit (2)
	current := &LocationOffset{
		Signature:       [64]byte{0},
		AuthorityPubkey: [32]byte{0},
		MeasurementSlot: 0,
		MeasuredRttNs:   100,
		Lat:             1.0,
		Lng:             2.0,
		RttNs:           100,
		NumReferences:   0,
		References:      nil,
	}

	// Build a chain of depth 2 (at the limit)
	for i := 1; i <= 2; i++ {
		parent := &LocationOffset{
			Signature:       [64]byte{byte(i)},
			AuthorityPubkey: [32]byte{byte(i)},
			MeasurementSlot: uint64(i),
			MeasuredRttNs:   uint64((i + 1) * 100),
			Lat:             1.0,
			Lng:             2.0,
			RttNs:           uint64((i + 1) * 100),
			NumReferences:   1,
			References:      []LocationOffset{*current},
		}
		current = parent
	}

	// Marshal and unmarshal should succeed
	data, err := current.Marshal()
	require.NoError(t, err)

	decoded := &LocationOffset{}
	err = decoded.Unmarshal(data)
	require.NoError(t, err)

	// Verify the chain depth
	depth := 0
	ptr := decoded
	for ptr.NumReferences > 0 {
		depth++
		ptr = &ptr.References[0]
	}
	require.Equal(t, 2, depth)
}

func TestLocationOffset_CountTotalReferences(t *testing.T) {
	t.Parallel()

	// Create a simple chain
	leaf := &LocationOffset{
		NumReferences: 0,
		References:    nil,
	}

	mid := &LocationOffset{
		NumReferences: 1,
		References:    []LocationOffset{*leaf},
	}

	root := &LocationOffset{
		NumReferences: 1,
		References:    []LocationOffset{*mid},
	}

	// Count should be 1 (mid) + 1 (leaf from mid) = 2
	require.Equal(t, 2, root.countTotalReferences())

	// Create a wide tree
	children := make([]LocationOffset, 3)
	for i := 0; i < 3; i++ {
		children[i] = LocationOffset{
			NumReferences: 0,
			References:    nil,
		}
	}

	parent := &LocationOffset{
		NumReferences: 3,
		References:    children,
	}

	// Count should be 3 (direct children)
	require.Equal(t, 3, parent.countTotalReferences())
}
