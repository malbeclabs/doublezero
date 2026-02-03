package allocation

import (
	"context"
	"fmt"

	"github.com/gagliardetto/solana-go"
	"github.com/malbeclabs/doublezero/smartcontract/sdk/go/serviceability"
	"github.com/mr-tron/base58"
)

// AllocationState represents the allocation state of a resource pool
type AllocationState struct {
	Allocated int
	Available int
	Total     int
}

// ResourceSnapshot captures the state of all resource pools at a point in time
type ResourceSnapshot struct {
	// Global resource pools
	UserTunnelBlock     *AllocationState
	DeviceTunnelBlock   *AllocationState
	LinkIds             *AllocationState
	MulticastGroupBlock *AllocationState

	// Device-specific resource pools (keyed by device pubkey base58)
	TunnelIds      map[string]*AllocationState // TunnelIds[device_pubkey]
	DzPrefixBlocks map[string]*AllocationState // DzPrefixBlocks[device_pubkey:index]
}

// Verifier provides methods to capture and compare ResourceExtension states
type Verifier struct {
	client serviceability.ProgramDataProvider
}

// NewVerifier creates a new allocation verifier
func NewVerifier(client serviceability.ProgramDataProvider) *Verifier {
	return &Verifier{client: client}
}

// CaptureSnapshot captures the current state of all ResourceExtensions
func (v *Verifier) CaptureSnapshot(ctx context.Context) (*ResourceSnapshot, error) {
	data, err := v.client.GetProgramData(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get program data: %w", err)
	}

	snapshot := &ResourceSnapshot{
		TunnelIds:      make(map[string]*AllocationState),
		DzPrefixBlocks: make(map[string]*AllocationState),
	}

	for _, ext := range data.ResourceExtensions {
		state := &AllocationState{
			Allocated: ext.AllocatedCount(),
			Available: ext.AvailableCount(),
			Total:     ext.TotalCapacity(),
		}

		// Classify the resource extension based on its properties
		// Global pools have associated_with set to default pubkey (all zeros)
		isGlobal := isDefaultPubkey(ext.AssociatedWith)

		if ext.Allocator.Type == serviceability.AllocatorTypeIp {
			if isGlobal {
				// Determine which global IP pool this is by checking the base network
				// UserTunnelBlock, DeviceTunnelBlock, and MulticastGroupBlock are all IP allocators
				// We'll need to check against the Config to identify them
				baseNet := ext.BaseNetString()
				if baseNet != "" {
					// Check against config to identify the pool type
					configUserTunnelBlock := onChainNetToString(data.Config.UserTunnelBlock)
					configDeviceTunnelBlock := onChainNetToString(data.Config.TunnelTunnelBlock)
					configMulticastBlock := onChainNetToString(data.Config.MulticastGroupBlock)

					if baseNet == configUserTunnelBlock {
						snapshot.UserTunnelBlock = state
					} else if baseNet == configDeviceTunnelBlock {
						snapshot.DeviceTunnelBlock = state
					} else if baseNet == configMulticastBlock {
						snapshot.MulticastGroupBlock = state
					}
				}
			} else {
				// Device-specific DzPrefixBlock
				deviceKey := base58.Encode(ext.AssociatedWith[:])
				// Use the pubkey as the unique identifier since there can be multiple DzPrefixBlocks per device
				extKey := fmt.Sprintf("%s:%s", deviceKey, base58.Encode(ext.PubKey[:]))
				snapshot.DzPrefixBlocks[extKey] = state
			}
		} else if ext.Allocator.Type == serviceability.AllocatorTypeId {
			if isGlobal {
				// Distinguish between global ID allocators by their range start:
				// - LinkIds: RangeStart=0, RangeEnd=65535
				// - SegmentRoutingIds: RangeStart=1, RangeEnd=65535
				if ext.Allocator.IdAllocator != nil && ext.Allocator.IdAllocator.RangeStart == 0 {
					snapshot.LinkIds = state
				}
				// SegmentRoutingIds (RangeStart=1) is ignored for now
			} else {
				// Device-specific TunnelIds
				deviceKey := base58.Encode(ext.AssociatedWith[:])
				snapshot.TunnelIds[deviceKey] = state
			}
		}
	}

	return snapshot, nil
}

// AssertAllocated verifies that the expected number of resources were allocated
func (v *Verifier) AssertAllocated(before, after *ResourceSnapshot, resourceType string, expectedCount int) error {
	beforeState, afterState, err := v.getStates(before, after, resourceType)
	if err != nil {
		return err
	}

	actualAllocated := afterState.Allocated - beforeState.Allocated
	if actualAllocated != expectedCount {
		return fmt.Errorf("%s: expected %d resources to be allocated, but %d were allocated (before=%d, after=%d)",
			resourceType, expectedCount, actualAllocated, beforeState.Allocated, afterState.Allocated)
	}

	return nil
}

// AssertDeallocated verifies that the expected number of resources were deallocated
func (v *Verifier) AssertDeallocated(before, after *ResourceSnapshot, resourceType string, expectedCount int) error {
	beforeState, afterState, err := v.getStates(before, after, resourceType)
	if err != nil {
		return err
	}

	actualDeallocated := beforeState.Allocated - afterState.Allocated
	if actualDeallocated != expectedCount {
		return fmt.Errorf("%s: expected %d resources to be deallocated, but %d were deallocated (before=%d, after=%d)",
			resourceType, expectedCount, actualDeallocated, beforeState.Allocated, afterState.Allocated)
	}

	return nil
}

// AssertResourcesReturned verifies that resources have been returned to their pre-allocation state
func (v *Verifier) AssertResourcesReturned(beforeAlloc, afterDealloc *ResourceSnapshot) error {
	// Check global pools
	if err := v.assertStateEqual(beforeAlloc.UserTunnelBlock, afterDealloc.UserTunnelBlock, "UserTunnelBlock"); err != nil {
		return err
	}
	if err := v.assertStateEqual(beforeAlloc.DeviceTunnelBlock, afterDealloc.DeviceTunnelBlock, "DeviceTunnelBlock"); err != nil {
		return err
	}
	if err := v.assertStateEqual(beforeAlloc.LinkIds, afterDealloc.LinkIds, "LinkIds"); err != nil {
		return err
	}
	if err := v.assertStateEqual(beforeAlloc.MulticastGroupBlock, afterDealloc.MulticastGroupBlock, "MulticastGroupBlock"); err != nil {
		return err
	}

	// Check device-specific pools
	for key, beforeState := range beforeAlloc.TunnelIds {
		afterState, ok := afterDealloc.TunnelIds[key]
		if !ok {
			// Pool might have been closed if device was deleted
			continue
		}
		if err := v.assertStateEqual(beforeState, afterState, fmt.Sprintf("TunnelIds[%s]", key)); err != nil {
			return err
		}
	}

	for key, beforeState := range beforeAlloc.DzPrefixBlocks {
		afterState, ok := afterDealloc.DzPrefixBlocks[key]
		if !ok {
			// Pool might have been closed if device was deleted
			continue
		}
		if err := v.assertStateEqual(beforeState, afterState, fmt.Sprintf("DzPrefixBlock[%s]", key)); err != nil {
			return err
		}
	}

	return nil
}

// GetState returns the allocation state for a specific resource type from a snapshot
func (v *Verifier) GetState(snapshot *ResourceSnapshot, resourceType string) (*AllocationState, error) {
	switch resourceType {
	case "UserTunnelBlock":
		if snapshot.UserTunnelBlock == nil {
			return nil, fmt.Errorf("UserTunnelBlock not found in snapshot")
		}
		return snapshot.UserTunnelBlock, nil
	case "DeviceTunnelBlock":
		if snapshot.DeviceTunnelBlock == nil {
			return nil, fmt.Errorf("DeviceTunnelBlock not found in snapshot")
		}
		return snapshot.DeviceTunnelBlock, nil
	case "LinkIds":
		if snapshot.LinkIds == nil {
			return nil, fmt.Errorf("LinkIds not found in snapshot")
		}
		return snapshot.LinkIds, nil
	case "MulticastGroupBlock":
		if snapshot.MulticastGroupBlock == nil {
			return nil, fmt.Errorf("MulticastGroupBlock not found in snapshot")
		}
		return snapshot.MulticastGroupBlock, nil
	default:
		// Check for device-specific pools with format "TunnelIds:device_pubkey" or "DzPrefixBlock:device_pubkey:index"
		if len(resourceType) > 10 && resourceType[:10] == "TunnelIds:" {
			deviceKey := resourceType[10:]
			state, ok := snapshot.TunnelIds[deviceKey]
			if !ok {
				return nil, fmt.Errorf("TunnelIds[%s] not found in snapshot", deviceKey)
			}
			return state, nil
		}
		if len(resourceType) > 14 && resourceType[:14] == "DzPrefixBlock:" {
			key := resourceType[14:]
			state, ok := snapshot.DzPrefixBlocks[key]
			if !ok {
				return nil, fmt.Errorf("DzPrefixBlock[%s] not found in snapshot", key)
			}
			return state, nil
		}
		return nil, fmt.Errorf("unknown resource type: %s", resourceType)
	}
}

func (v *Verifier) getStates(before, after *ResourceSnapshot, resourceType string) (*AllocationState, *AllocationState, error) {
	beforeState, err := v.GetState(before, resourceType)
	if err != nil {
		return nil, nil, fmt.Errorf("before snapshot: %w", err)
	}

	afterState, err := v.GetState(after, resourceType)
	if err != nil {
		return nil, nil, fmt.Errorf("after snapshot: %w", err)
	}

	return beforeState, afterState, nil
}

func (v *Verifier) assertStateEqual(before, after *AllocationState, name string) error {
	if before == nil && after == nil {
		return nil
	}
	if before == nil {
		return fmt.Errorf("%s: state was nil before but exists after", name)
	}
	if after == nil {
		return fmt.Errorf("%s: state existed before but is nil after", name)
	}
	if before.Allocated != after.Allocated {
		return fmt.Errorf("%s: allocated count mismatch (before=%d, after=%d) - resources were not properly returned",
			name, before.Allocated, after.Allocated)
	}
	return nil
}

// isDefaultPubkey checks if the pubkey is all zeros (default/unset)
func isDefaultPubkey(pk [32]byte) bool {
	for _, b := range pk {
		if b != 0 {
			return false
		}
	}
	return true
}

// onChainNetToString converts a NetworkV4 ([5]byte) to a CIDR string
func onChainNetToString(n [5]uint8) string {
	prefixLen := n[4]
	if prefixLen > 0 && prefixLen <= 32 {
		ipBytes := n[:4]
		return fmt.Sprintf("%d.%d.%d.%d/%d", ipBytes[0], ipBytes[1], ipBytes[2], ipBytes[3], prefixLen)
	}
	return ""
}

// FindTunnelIdsForDevice finds the TunnelIds pool for a specific device
func (v *Verifier) FindTunnelIdsForDevice(snapshot *ResourceSnapshot, devicePubkey solana.PublicKey) (*AllocationState, error) {
	deviceKey := devicePubkey.String()
	state, ok := snapshot.TunnelIds[deviceKey]
	if !ok {
		return nil, fmt.Errorf("TunnelIds not found for device %s", deviceKey)
	}
	return state, nil
}

// FindDzPrefixBlocksForDevice finds all DzPrefixBlock pools for a specific device
func (v *Verifier) FindDzPrefixBlocksForDevice(snapshot *ResourceSnapshot, devicePubkey solana.PublicKey) []*AllocationState {
	deviceKey := devicePubkey.String()
	var results []*AllocationState
	for key, state := range snapshot.DzPrefixBlocks {
		// Key format is "devicePubkey:extPubkey"
		if len(key) > len(deviceKey)+1 && key[:len(deviceKey)] == deviceKey {
			results = append(results, state)
		}
	}
	return results
}

// GetTotalDzPrefixAllocatedForDevice returns the total allocated count across all DzPrefixBlocks for a device
func (v *Verifier) GetTotalDzPrefixAllocatedForDevice(snapshot *ResourceSnapshot, devicePubkey solana.PublicKey) int {
	blocks := v.FindDzPrefixBlocksForDevice(snapshot, devicePubkey)
	total := 0
	for _, block := range blocks {
		total += block.Allocated
	}
	return total
}

// AssertDeviceResourcesAllocated verifies that the expected device-specific resources were allocated
func (v *Verifier) AssertDeviceResourcesAllocated(before, after *ResourceSnapshot, devicePubkey solana.PublicKey, expectedTunnelIds, expectedDzPrefix int) error {
	// Check TunnelIds
	beforeTunnelIds, err := v.FindTunnelIdsForDevice(before, devicePubkey)
	if err != nil {
		return fmt.Errorf("before snapshot: %w", err)
	}
	afterTunnelIds, err := v.FindTunnelIdsForDevice(after, devicePubkey)
	if err != nil {
		return fmt.Errorf("after snapshot: %w", err)
	}
	actualTunnelIds := afterTunnelIds.Allocated - beforeTunnelIds.Allocated
	if actualTunnelIds != expectedTunnelIds {
		return fmt.Errorf("TunnelIds[%s]: expected %d resources to be allocated, but %d were allocated (before=%d, after=%d)",
			devicePubkey.String(), expectedTunnelIds, actualTunnelIds, beforeTunnelIds.Allocated, afterTunnelIds.Allocated)
	}

	// Check DzPrefixBlocks (sum across all blocks for the device)
	beforeDzPrefix := v.GetTotalDzPrefixAllocatedForDevice(before, devicePubkey)
	afterDzPrefix := v.GetTotalDzPrefixAllocatedForDevice(after, devicePubkey)
	actualDzPrefix := afterDzPrefix - beforeDzPrefix
	if actualDzPrefix != expectedDzPrefix {
		return fmt.Errorf("DzPrefixBlocks[%s]: expected %d resources to be allocated, but %d were allocated (before=%d, after=%d)",
			devicePubkey.String(), expectedDzPrefix, actualDzPrefix, beforeDzPrefix, afterDzPrefix)
	}

	return nil
}

// AssertDeviceResourcesDeallocated verifies that the expected device-specific resources were deallocated
func (v *Verifier) AssertDeviceResourcesDeallocated(before, after *ResourceSnapshot, devicePubkey solana.PublicKey, expectedTunnelIds, expectedDzPrefix int) error {
	// Check TunnelIds
	beforeTunnelIds, err := v.FindTunnelIdsForDevice(before, devicePubkey)
	if err != nil {
		return fmt.Errorf("before snapshot: %w", err)
	}
	afterTunnelIds, err := v.FindTunnelIdsForDevice(after, devicePubkey)
	if err != nil {
		return fmt.Errorf("after snapshot: %w", err)
	}
	actualTunnelIds := beforeTunnelIds.Allocated - afterTunnelIds.Allocated
	if actualTunnelIds != expectedTunnelIds {
		return fmt.Errorf("TunnelIds[%s]: expected %d resources to be deallocated, but %d were deallocated (before=%d, after=%d)",
			devicePubkey.String(), expectedTunnelIds, actualTunnelIds, beforeTunnelIds.Allocated, afterTunnelIds.Allocated)
	}

	// Check DzPrefixBlocks (sum across all blocks for the device)
	beforeDzPrefix := v.GetTotalDzPrefixAllocatedForDevice(before, devicePubkey)
	afterDzPrefix := v.GetTotalDzPrefixAllocatedForDevice(after, devicePubkey)
	actualDzPrefix := beforeDzPrefix - afterDzPrefix
	if actualDzPrefix != expectedDzPrefix {
		return fmt.Errorf("DzPrefixBlocks[%s]: expected %d resources to be deallocated, but %d were deallocated (before=%d, after=%d)",
			devicePubkey.String(), expectedDzPrefix, actualDzPrefix, beforeDzPrefix, afterDzPrefix)
	}

	return nil
}
