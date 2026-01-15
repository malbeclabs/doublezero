package gnmi

import (
	"strings"

	"github.com/malbeclabs/doublezero/telemetry/gnmi-writer/internal/gnmi/oc"
)

// DefaultExtractors is the standard set of extractors for processing gNMI notifications.
// Add new extractors here to enable collection of additional OpenConfig paths.
var DefaultExtractors = []ExtractorDef{
	{Name: "isis_adjacencies", Match: PathContains("isis", "adjacencies"), Extract: extractIsisAdjacencies},
	{Name: "system_state", Match: PathContains("system", "state"), Extract: extractSystemState},
	{Name: "bgp_neighbors", Match: PathContains("bgp", "neighbors"), Extract: extractBgpNeighbors},
	{Name: "interface_ifindex", Match: PathContains("interfaces", "ifindex"), Extract: extractInterfaceIfindex},
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
