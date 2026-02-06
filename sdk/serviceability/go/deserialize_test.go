package serviceability

import (
	"testing"
)

// TestDeserializeResourceExtensionIdAllocator tests deserializing a ResourceExtension with ID allocator
func TestDeserializeResourceExtensionIdAllocator(t *testing.T) {
	t.Parallel()

	// Build test data for an ID allocator ResourceExtension
	// Header layout (83 bytes for ID allocator, padded to 88 for alignment):
	//   [0] account_type = 12 (ResourceExtension)
	//   [1-32] owner pubkey (32 bytes)
	//   [33] bump_seed
	//   [34-65] associated_with pubkey (32 bytes)
	//   [66] allocator discriminant (1 = Id)
	//   [67-68] range_start (u16)
	//   [69-70] range_end (u16)
	//   [71-78] first_free_index (u64)
	//   [79-87] padding to align to 88 bytes
	//   [88+] bitmap storage

	data := make([]byte, 96) // 88 bytes header + 8 bytes bitmap

	// Account type = 12 (ResourceExtension)
	data[0] = 12

	// Owner pubkey (bytes 1-32)
	for i := 1; i <= 32; i++ {
		data[i] = byte(i)
	}

	// Bump seed
	data[33] = 255

	// Associated with pubkey (bytes 34-65) - all zeros for global resource
	// (already zeroed)

	// Allocator discriminant = 1 (Id)
	data[66] = 1

	// Range start = 0 (bytes 67-68, little endian)
	data[67] = 0
	data[68] = 0

	// Range end = 64 (bytes 69-70, little endian)
	data[69] = 64
	data[70] = 0

	// First free index = 0 (bytes 71-78, little endian u64)
	// (already zeroed)

	// Bitmap: set bits 0, 1, 2 as allocated (byte at offset 88)
	data[88] = 0x07 // bits 0, 1, 2 set

	reader := NewByteReader(data)
	var ext ResourceExtension
	DeserializeResourceExtension(reader, &ext)

	// Verify deserialization
	if ext.AccountType != ResourceExtensionType {
		t.Errorf("AccountType: got %d, expected %d", ext.AccountType, ResourceExtensionType)
	}

	if ext.BumpSeed != 255 {
		t.Errorf("BumpSeed: got %d, expected 255", ext.BumpSeed)
	}

	if ext.Allocator.Type != AllocatorTypeId {
		t.Errorf("Allocator.Type: got %d, expected %d (Id)", ext.Allocator.Type, AllocatorTypeId)
	}

	if ext.Allocator.IdAllocator == nil {
		t.Fatal("IdAllocator should not be nil")
	}

	if ext.Allocator.IdAllocator.RangeStart != 0 {
		t.Errorf("RangeStart: got %d, expected 0", ext.Allocator.IdAllocator.RangeStart)
	}

	if ext.Allocator.IdAllocator.RangeEnd != 64 {
		t.Errorf("RangeEnd: got %d, expected 64", ext.Allocator.IdAllocator.RangeEnd)
	}

	// Test capacity and allocation counting
	if ext.TotalCapacity() != 64 {
		t.Errorf("TotalCapacity: got %d, expected 64", ext.TotalCapacity())
	}

	if ext.AllocatedCount() != 3 {
		t.Errorf("AllocatedCount: got %d, expected 3", ext.AllocatedCount())
	}

	if ext.AvailableCount() != 61 {
		t.Errorf("AvailableCount: got %d, expected 61", ext.AvailableCount())
	}
}

// TestDeserializeResourceExtensionIpAllocator tests deserializing a ResourceExtension with IP allocator
func TestDeserializeResourceExtensionIpAllocator(t *testing.T) {
	t.Parallel()

	// Build test data for an IP allocator ResourceExtension
	// Header layout (84 bytes for IP allocator, padded to 88 for alignment):
	//   [0] account_type = 12 (ResourceExtension)
	//   [1-32] owner pubkey (32 bytes)
	//   [33] bump_seed
	//   [34-65] associated_with pubkey (32 bytes)
	//   [66] allocator discriminant (0 = Ip)
	//   [67-70] base_net IP (4 bytes)
	//   [71] base_net prefix (1 byte)
	//   [72-79] first_free_index (u64)
	//   [80-87] padding to align to 88 bytes
	//   [88+] bitmap storage

	data := make([]byte, 96) // 88 bytes header + 8 bytes bitmap (for /29 = 8 IPs)

	// Account type = 12 (ResourceExtension)
	data[0] = 12

	// Owner pubkey (bytes 1-32)
	for i := 1; i <= 32; i++ {
		data[i] = byte(i)
	}

	// Bump seed
	data[33] = 254

	// Associated with pubkey (bytes 34-65)
	for i := 34; i <= 65; i++ {
		data[i] = byte(i - 33) // Different from owner
	}

	// Allocator discriminant = 0 (Ip)
	data[66] = 0

	// Base net: 192.168.1.0/29 (bytes 67-71)
	data[67] = 192
	data[68] = 168
	data[69] = 1
	data[70] = 0
	data[71] = 29 // /29 prefix = 8 addresses

	// First free index = 0 (bytes 72-79, little endian u64)
	// (already zeroed)

	// Bitmap: set bits 0, 1, 2, 3 as allocated (byte at offset 88)
	data[88] = 0x0F // bits 0, 1, 2, 3 set

	reader := NewByteReader(data)
	var ext ResourceExtension
	DeserializeResourceExtension(reader, &ext)

	// Verify deserialization
	if ext.AccountType != ResourceExtensionType {
		t.Errorf("AccountType: got %d, expected %d", ext.AccountType, ResourceExtensionType)
	}

	if ext.BumpSeed != 254 {
		t.Errorf("BumpSeed: got %d, expected 254", ext.BumpSeed)
	}

	if ext.Allocator.Type != AllocatorTypeIp {
		t.Errorf("Allocator.Type: got %d, expected %d (Ip)", ext.Allocator.Type, AllocatorTypeIp)
	}

	if ext.Allocator.IpAllocator == nil {
		t.Fatal("IpAllocator should not be nil")
	}

	// Verify base net
	expectedBaseNet := [5]byte{192, 168, 1, 0, 29}
	if ext.Allocator.IpAllocator.BaseNet != expectedBaseNet {
		t.Errorf("BaseNet: got %v, expected %v", ext.Allocator.IpAllocator.BaseNet, expectedBaseNet)
	}

	// Test capacity and allocation counting
	// /29 = 32 - 29 = 3 bits = 8 addresses
	if ext.TotalCapacity() != 8 {
		t.Errorf("TotalCapacity: got %d, expected 8", ext.TotalCapacity())
	}

	if ext.AllocatedCount() != 4 {
		t.Errorf("AllocatedCount: got %d, expected 4", ext.AllocatedCount())
	}

	if ext.AvailableCount() != 4 {
		t.Errorf("AvailableCount: got %d, expected 4", ext.AvailableCount())
	}

	// Test BaseNetString
	baseNetStr := ext.BaseNetString()
	if baseNetStr != "192.168.1.0/29" {
		t.Errorf("BaseNetString: got %s, expected 192.168.1.0/29", baseNetStr)
	}
}
