package gnmi

import (
	"strings"

	"github.com/malbeclabs/doublezero/telemetry/gnmi-writer/internal/gnmi/oc"
)

// DefaultExtractors is the standard set of extractors for processing gNMI notifications.
// Add new extractors here to enable collection of additional OpenConfig paths.
//
// IMPORTANT: Order matters! When multiple extractors match a path, the first one wins.
// Place more specific matchers before less specific ones to avoid collisions.
// For example, interface_ifindex (matches "interfaces" + "ifindex") must come before
// interface_state (matches "interfaces" + "interface" + "state") since ifindex paths
// contain all three elements.
var DefaultExtractors = []ExtractorDef{
	{Name: "isis_adjacencies", Match: PathContains("isis", "adjacencies"), Extract: extractIsisAdjacencies},
	{Name: "system_state", Match: PathContains("system", "state"), Extract: extractSystemState},
	{Name: "bgp_neighbors", Match: PathContains("bgp", "neighbors"), Extract: extractBgpNeighbors},
	{Name: "interface_ifindex", Match: PathContains("interfaces", "ifindex"), Extract: extractInterfaceIfindex},
	{Name: "transceiver_state", Match: PathContains("transceiver", "physical-channels"), Extract: extractTransceiverState},
	{Name: "transceiver_thresholds", Match: PathContains("transceiver", "thresholds"), Extract: extractTransceiverThresholds},
	{Name: "interface_state", Match: PathContains("interfaces", "interface", "state"), Extract: extractInterfaceState},
}

// extractIsisAdjacencies extracts ISIS adjacency records from an oc.Device.
func extractIsisAdjacencies(device *oc.Device, meta Metadata) []Record {
	var records []Record

	if device.NetworkInstances == nil {
		return nil
	}

	for _, ni := range device.NetworkInstances.NetworkInstance {
		if ni.Protocols == nil {
			continue
		}
		for _, proto := range ni.Protocols.Protocol {
			if proto.Isis == nil || proto.Isis.Interfaces == nil {
				continue
			}
			for ifID, iface := range proto.Isis.Interfaces.Interface {
				if iface.Levels == nil {
					continue
				}
				for levelNum, level := range iface.Levels.Level {
					if level.Adjacencies == nil {
						continue
					}
					for sysID, adj := range level.Adjacencies.Adjacency {
						record := IsisAdjacencyRecord{
							Timestamp:    meta.Timestamp,
							DevicePubkey: meta.DevicePubkey,
							InterfaceID:  ifID,
							Level:        uint8(levelNum),
							SystemID:     sysID,
						}

						// All adjacency fields are now in State
						if adj.State != nil {
							if adj.State.AdjacencyState != 0 {
								record.AdjacencyState = adj.State.AdjacencyState.String()
							}
							if adj.State.NeighborIpv4Address != nil {
								record.NeighborIPv4 = *adj.State.NeighborIpv4Address
							}
							if adj.State.NeighborIpv6Address != nil && *adj.State.NeighborIpv6Address != "::" {
								record.NeighborIPv6 = *adj.State.NeighborIpv6Address
							}
							if adj.State.NeighborCircuitType != 0 {
								record.NeighborCircuitType = adj.State.NeighborCircuitType.String()
							}
							if len(adj.State.AreaAddress) > 0 {
								record.AreaAddress = strings.Join(adj.State.AreaAddress, ",")
							}
							if adj.State.UpTimestamp != nil {
								record.UpTimestamp = int64(*adj.State.UpTimestamp)
							}
							if adj.State.LocalExtendedCircuitId != nil {
								record.LocalCircuitID = *adj.State.LocalExtendedCircuitId
							}
							if adj.State.NeighborExtendedCircuitId != nil {
								record.NeighborCircuitID = *adj.State.NeighborExtendedCircuitId
							}
						}

						records = append(records, record)
					}
				}
			}
		}
	}

	return records
}

// extractSystemState extracts system state records from an oc.Device.
func extractSystemState(device *oc.Device, meta Metadata) []Record {
	if device.System == nil {
		return nil
	}

	record := SystemStateRecord{
		Timestamp:    meta.Timestamp,
		DevicePubkey: meta.DevicePubkey,
	}

	// Hostname is now in State container
	if device.System.State != nil && device.System.State.Hostname != nil {
		record.Hostname = *device.System.State.Hostname
	}

	// Memory is now in Memory.State
	if device.System.Memory != nil && device.System.Memory.State != nil {
		if device.System.Memory.State.Physical != nil {
			record.MemTotal = *device.System.Memory.State.Physical
		}
		if device.System.Memory.State.Used != nil {
			record.MemUsed = *device.System.Memory.State.Used
		}
		if device.System.Memory.State.Free != nil {
			record.MemFree = *device.System.Memory.State.Free
		}
	}

	// CPU is now in Cpus.Cpu[].State
	if device.System.Cpus != nil {
		var totalUser, totalSystem, totalIdle float64
		var userCount, systemCount, idleCount int
		for _, cpu := range device.System.Cpus.Cpu {
			if cpu.State == nil {
				continue
			}
			if cpu.State.User != nil && cpu.State.User.Instant != nil {
				userCount++
				totalUser += float64(*cpu.State.User.Instant)
			}
			if cpu.State.Kernel != nil && cpu.State.Kernel.Instant != nil {
				systemCount++
				totalSystem += float64(*cpu.State.Kernel.Instant)
			}
			if cpu.State.Idle != nil && cpu.State.Idle.Instant != nil {
				idleCount++
				totalIdle += float64(*cpu.State.Idle.Instant)
			}
		}
		if userCount > 0 {
			record.CpuUser = totalUser / float64(userCount)
		}
		if systemCount > 0 {
			record.CpuSystem = totalSystem / float64(systemCount)
		}
		if idleCount > 0 {
			record.CpuIdle = totalIdle / float64(idleCount)
		}
	}

	// Only return a record if we extracted something meaningful
	if record.Hostname == "" && record.MemTotal == 0 && record.CpuUser == 0 {
		return nil
	}

	return []Record{record}
}

// extractBgpNeighbors extracts BGP neighbor records from an oc.Device.
func extractBgpNeighbors(device *oc.Device, meta Metadata) []Record {
	var records []Record

	if device.NetworkInstances == nil {
		return nil
	}

	for niName, ni := range device.NetworkInstances.NetworkInstance {
		if ni.Protocols == nil {
			continue
		}
		for _, proto := range ni.Protocols.Protocol {
			if proto.Bgp == nil || proto.Bgp.Neighbors == nil {
				continue
			}
			for addr, neighbor := range proto.Bgp.Neighbors.Neighbor {
				record := BgpNeighborRecord{
					Timestamp:       meta.Timestamp,
					DevicePubkey:    meta.DevicePubkey,
					NetworkInstance: niName,
					NeighborAddress: addr,
				}

				// All neighbor fields are now in State
				if neighbor.State != nil {
					if neighbor.State.Description != nil {
						record.Description = *neighbor.State.Description
					}
					if neighbor.State.PeerAs != nil {
						record.PeerAs = *neighbor.State.PeerAs
					}
					if neighbor.State.LocalAs != nil {
						record.LocalAs = *neighbor.State.LocalAs
					}
					if neighbor.State.PeerType != 0 {
						record.PeerType = neighbor.State.PeerType.String()
					}
					if neighbor.State.SessionState != 0 {
						record.SessionState = neighbor.State.SessionState.String()
					}
					if neighbor.State.EstablishedTransitions != nil {
						record.EstablishedTransitions = *neighbor.State.EstablishedTransitions
					}
					if neighbor.State.LastEstablished != nil {
						record.LastEstablished = int64(*neighbor.State.LastEstablished)
					}
					if neighbor.State.Messages != nil {
						if neighbor.State.Messages.Received != nil && neighbor.State.Messages.Received.UPDATE != nil {
							record.MessagesReceivedUpdate = *neighbor.State.Messages.Received.UPDATE
						}
						if neighbor.State.Messages.Sent != nil && neighbor.State.Messages.Sent.UPDATE != nil {
							record.MessagesSentUpdate = *neighbor.State.Messages.Sent.UPDATE
						}
					}
				}

				records = append(records, record)
			}
		}
	}

	return records
}

// extractInterfaceIfindex extracts interface ifindex mappings from an oc.Device.
// Extracts from /interfaces/interface/state/ifindex.
func extractInterfaceIfindex(device *oc.Device, meta Metadata) []Record {
	var records []Record

	if device.Interfaces == nil {
		return nil
	}

	for ifName, iface := range device.Interfaces.Interface {
		if iface.State == nil || iface.State.Ifindex == nil {
			continue
		}
		record := InterfaceIfindexRecord{
			Timestamp:     meta.Timestamp,
			DevicePubkey:  meta.DevicePubkey,
			InterfaceName: ifName,
			Ifindex:       *iface.State.Ifindex,
		}
		records = append(records, record)
	}

	return records
}

// extractTransceiverState extracts optical transceiver channel state from an oc.Device.
func extractTransceiverState(device *oc.Device, meta Metadata) []Record {
	var records []Record

	if device.Components == nil {
		return nil
	}

	for compName, comp := range device.Components.Component {
		if comp.Transceiver == nil || comp.Transceiver.PhysicalChannels == nil {
			continue
		}
		for chanIdx, channel := range comp.Transceiver.PhysicalChannels.Channel {
			if channel.State == nil {
				continue
			}

			record := TransceiverStateRecord{
				Timestamp:     meta.Timestamp,
				DevicePubkey:  meta.DevicePubkey,
				InterfaceName: compName,
				ChannelIndex:  chanIdx,
			}

			// Check if any power metrics are present in the update.
			// Skip updates that only contain other fields (e.g., description).
			hasInputPower := channel.State.InputPower != nil && channel.State.InputPower.Instant != nil
			hasOutputPower := channel.State.OutputPower != nil && channel.State.OutputPower.Instant != nil
			hasLaserBias := channel.State.LaserBiasCurrent != nil && channel.State.LaserBiasCurrent.Instant != nil

			if !hasInputPower && !hasOutputPower && !hasLaserBias {
				continue
			}

			// Extract optical power metrics
			if hasInputPower {
				record.InputPower = *channel.State.InputPower.Instant
			}
			if hasOutputPower {
				record.OutputPower = *channel.State.OutputPower.Instant
			}
			if hasLaserBias {
				record.LaserBiasCurrent = *channel.State.LaserBiasCurrent.Instant
			}

			records = append(records, record)
		}
	}

	return records
}

// extractInterfaceState extracts interface state records from an oc.Device.
func extractInterfaceState(device *oc.Device, meta Metadata) []Record {
	var records []Record

	if device.Interfaces == nil {
		return nil
	}

	for ifName, iface := range device.Interfaces.Interface {
		if iface.State == nil {
			continue
		}

		record := InterfaceStateRecord{
			Timestamp:     meta.Timestamp,
			DevicePubkey:  meta.DevicePubkey,
			InterfaceName: ifName,
		}

		// Extract state fields
		if iface.State.AdminStatus != 0 {
			record.AdminStatus = iface.State.AdminStatus.String()
		}
		if iface.State.OperStatus != 0 {
			record.OperStatus = iface.State.OperStatus.String()
		}
		if iface.State.Ifindex != nil {
			record.Ifindex = *iface.State.Ifindex
		}
		if iface.State.Mtu != nil {
			record.Mtu = *iface.State.Mtu
		}
		if iface.State.LastChange != nil {
			record.LastChange = int64(*iface.State.LastChange)
		}

		// Extract counters
		if iface.State.Counters != nil {
			if iface.State.Counters.CarrierTransitions != nil {
				record.CarrierTransitions = *iface.State.Counters.CarrierTransitions
			}
			if iface.State.Counters.InOctets != nil {
				record.InOctets = *iface.State.Counters.InOctets
			}
			if iface.State.Counters.OutOctets != nil {
				record.OutOctets = *iface.State.Counters.OutOctets
			}
			if iface.State.Counters.InPkts != nil {
				record.InPkts = *iface.State.Counters.InPkts
			}
			if iface.State.Counters.OutPkts != nil {
				record.OutPkts = *iface.State.Counters.OutPkts
			}
			if iface.State.Counters.InErrors != nil {
				record.InErrors = *iface.State.Counters.InErrors
			}
			if iface.State.Counters.OutErrors != nil {
				record.OutErrors = *iface.State.Counters.OutErrors
			}
			if iface.State.Counters.InDiscards != nil {
				record.InDiscards = *iface.State.Counters.InDiscards
			}
			if iface.State.Counters.OutDiscards != nil {
				record.OutDiscards = *iface.State.Counters.OutDiscards
			}
		}

		records = append(records, record)
	}

	return records
}

// extractTransceiverThresholds extracts transceiver alarm thresholds from an oc.Device.
func extractTransceiverThresholds(device *oc.Device, meta Metadata) []Record {
	var records []Record

	if device.Components == nil {
		return nil
	}

	for compName, comp := range device.Components.Component {
		if comp.Transceiver == nil || comp.Transceiver.Thresholds == nil {
			continue
		}
		for severity, threshold := range comp.Transceiver.Thresholds.Threshold {
			if threshold.State == nil {
				continue
			}

			record := TransceiverThresholdRecord{
				Timestamp:     meta.Timestamp,
				DevicePubkey:  meta.DevicePubkey,
				InterfaceName: compName,
				Severity:      severity.String(),
			}

			// Extract threshold values
			if threshold.State.InputPowerLower != nil {
				record.InputPowerLower = *threshold.State.InputPowerLower
			}
			if threshold.State.InputPowerUpper != nil {
				record.InputPowerUpper = *threshold.State.InputPowerUpper
			}
			if threshold.State.OutputPowerLower != nil {
				record.OutputPowerLower = *threshold.State.OutputPowerLower
			}
			if threshold.State.OutputPowerUpper != nil {
				record.OutputPowerUpper = *threshold.State.OutputPowerUpper
			}
			if threshold.State.LaserBiasCurrentLower != nil {
				record.LaserBiasCurrentLower = *threshold.State.LaserBiasCurrentLower
			}
			if threshold.State.LaserBiasCurrentUpper != nil {
				record.LaserBiasCurrentUpper = *threshold.State.LaserBiasCurrentUpper
			}
			if threshold.State.ModuleTemperatureLower != nil {
				record.ModuleTemperatureLower = *threshold.State.ModuleTemperatureLower
			}
			if threshold.State.ModuleTemperatureUpper != nil {
				record.ModuleTemperatureUpper = *threshold.State.ModuleTemperatureUpper
			}
			if threshold.State.SupplyVoltageLower != nil {
				record.SupplyVoltageLower = *threshold.State.SupplyVoltageLower
			}
			if threshold.State.SupplyVoltageUpper != nil {
				record.SupplyVoltageUpper = *threshold.State.SupplyVoltageUpper
			}

			records = append(records, record)
		}
	}

	return records
}
