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

	for _, ni := range device.NetworkInstance {
		for _, proto := range ni.Protocol {
			if proto.Isis == nil {
				continue
			}
			for ifID, iface := range proto.Isis.Interface {
				for levelNum, level := range iface.Level {
					for sysID, adj := range level.Adjacency {
						record := IsisAdjacencyRecord{
							Timestamp:   meta.Timestamp,
							DeviceCode:  meta.DeviceCode,
							InterfaceID: ifID,
							Level:       uint8(levelNum),
							SystemID:    sysID,
						}

						if adj.AdjacencyState != 0 {
							record.AdjacencyState = adj.AdjacencyState.String()
						}
						if adj.NeighborIpv4Address != nil {
							record.NeighborIPv4 = *adj.NeighborIpv4Address
						}
						if adj.NeighborIpv6Address != nil && *adj.NeighborIpv6Address != "::" {
							record.NeighborIPv6 = *adj.NeighborIpv6Address
						}
						if adj.NeighborCircuitType != 0 {
							record.NeighborCircuitType = adj.NeighborCircuitType.String()
						}
						if len(adj.AreaAddress) > 0 {
							record.AreaAddress = strings.Join(adj.AreaAddress, ",")
						}
						if adj.UpTimestamp != nil {
							record.UpTimestamp = int64(*adj.UpTimestamp)
						}
						if adj.LocalExtendedCircuitId != nil {
							record.LocalCircuitID = *adj.LocalExtendedCircuitId
						}
						if adj.NeighborExtendedCircuitId != nil {
							record.NeighborCircuitID = *adj.NeighborExtendedCircuitId
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
		Timestamp:  meta.Timestamp,
		DeviceCode: meta.DeviceCode,
	}

	if device.System.Hostname != nil {
		record.Hostname = *device.System.Hostname
	}

	if device.System.Memory != nil {
		if device.System.Memory.Physical != nil {
			record.MemTotal = *device.System.Memory.Physical
		}
		if device.System.Memory.Used != nil {
			record.MemUsed = *device.System.Memory.Used
		}
		if device.System.Memory.Free != nil {
			record.MemFree = *device.System.Memory.Free
		}
	}

	if device.System.Cpu != nil {
		var totalUser, totalSystem, totalIdle float64
		var userCount, systemCount, idleCount int
		for _, cpu := range device.System.Cpu {
			if cpu.User != nil && cpu.User.Instant != nil {
				userCount++
				totalUser += float64(*cpu.User.Instant)
			}
			if cpu.Kernel != nil && cpu.Kernel.Instant != nil {
				systemCount++
				totalSystem += float64(*cpu.Kernel.Instant)
			}
			if cpu.Idle != nil && cpu.Idle.Instant != nil {
				idleCount++
				totalIdle += float64(*cpu.Idle.Instant)
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

	for _, ni := range device.NetworkInstance {
		for _, proto := range ni.Protocol {
			if proto.Bgp == nil {
				continue
			}
			for addr, neighbor := range proto.Bgp.Neighbor {
				record := BgpNeighborRecord{
					Timestamp:       meta.Timestamp,
					DeviceCode:      meta.DeviceCode,
					NeighborAddress: addr,
				}

				if neighbor.PeerAs != nil {
					record.PeerAs = *neighbor.PeerAs
				}
				if neighbor.SessionState != 0 {
					record.SessionState = neighbor.SessionState.String()
				}
				if neighbor.Enabled != nil {
					record.Enabled = *neighbor.Enabled
				}

				records = append(records, record)
			}
		}
	}

	return records
}

// extractInterfaceIfindex extracts interface ifindex mappings from an oc.Device.
func extractInterfaceIfindex(device *oc.Device, meta Metadata) []Record {
	var records []Record

	for ifName, iface := range device.Interface {
		for subifIdx, subif := range iface.Subinterface {
			if subif.Ifindex == nil {
				continue
			}
			record := InterfaceIfindexRecord{
				Timestamp:     meta.Timestamp,
				DeviceCode:    meta.DeviceCode,
				InterfaceName: ifName,
				SubifIndex:    subifIdx,
				Ifindex:       *subif.Ifindex,
			}
			records = append(records, record)
		}
	}

	return records
}
