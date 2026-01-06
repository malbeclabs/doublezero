// Package isis provides ISIS LSP JSON parsing and enrichment for LLM consumption.
package isis

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// JSON structure types - only fields we actually use (partial structs)

type jsonRoot struct {
	VRFs map[string]jsonVRF `json:"vrfs"`
}

type jsonVRF struct {
	ISISInstances map[string]jsonISISInstance `json:"isisInstances"`
}

type jsonISISInstance struct {
	Level map[string]jsonLevel `json:"level"`
}

type jsonLevel struct {
	LSPs map[string]jsonLSP `json:"lsps"`
}

type jsonLSP struct {
	Hostname           jsonHostname           `json:"hostname"`
	IntermediateSystem string                 `json:"intermediateSystemType"`
	AreaAddresses      []jsonAreaAddress      `json:"areaAddresses"`
	Flags              jsonFlags              `json:"flags"`
	Sequence           int                    `json:"sequence"`
	InterfaceAddresses []jsonInterfaceAddr    `json:"interfaceAddresses"`
	Neighbors          []jsonNeighbor         `json:"neighbors"`
	Reachabilities     []jsonReachability     `json:"reachabilities"`
	RouterCapabilities []jsonRouterCapability `json:"routerCapabilities"`
}

type jsonHostname struct {
	Name string `json:"name"`
}

type jsonAreaAddress struct {
	Address string `json:"address"`
}

type jsonFlags struct {
	DBOverload bool `json:"dbOverload"`
}

type jsonInterfaceAddr struct {
	IPv4Address string `json:"ipv4Address"`
}

type jsonNeighbor struct {
	SystemID     string       `json:"systemId"`
	Metric       int          `json:"metric"`
	NeighborAddr string       `json:"neighborAddr"`
	AdjSIDs      []jsonAdjSID `json:"adjSids"`
}

type jsonAdjSID struct {
	AdjSID int `json:"adjSid"`
}

type jsonReachability struct {
	ReachabilityV4Addr     string                     `json:"reachabilityV4Addr"`
	MaskLength             int                        `json:"maskLength"`
	Metric                 int                        `json:"metric"`
	SRPrefixReachabilities []jsonSRPrefixReachability `json:"srPrefixReachabilities"`
}

type jsonSRPrefixReachability struct {
	SID     int              `json:"sid"`
	Options jsonSRPrefixOpts `json:"options"`
}

type jsonSRPrefixOpts struct {
	NodeSID bool `json:"nodeSID"`
}

type jsonRouterCapability struct {
	RouterID       string             `json:"routerId"`
	SRCapabilities []jsonSRCapability `json:"srCapabilities"`
	SRLB           jsonSRLB           `json:"srlb"`
	MSD            jsonMSD            `json:"msd"`
}

type jsonSRCapability struct {
	SRCapabilitySRGB []jsonSRGBRange `json:"srCapabilitySrgb"`
	SRGBRanges       []jsonSRGBRange `json:"srgbRanges"`
}

type jsonSRGBRange struct {
	SRGBBase  int `json:"srgbBase"`
	SRGBRange int `json:"srgbRange"`
}

type jsonSRLB struct {
	SRLBRanges []jsonSRLBRange `json:"srlbRanges"`
}

type jsonSRLBRange struct {
	SRLBBase  int `json:"srlbBase"`
	SRLBRange int `json:"srlbRange"`
}

type jsonMSD struct {
	BaseMPLSImposition int `json:"baseMplsImposition"`
}

// parseLSPs extracts LSPs from JSON at the specified ISIS level.
func parseLSPs(r io.Reader, level int) (map[string]jsonLSP, error) {
	var root jsonRoot
	if err := json.NewDecoder(r).Decode(&root); err != nil {
		return nil, fmt.Errorf("failed to decode JSON: %w", err)
	}

	vrf, ok := root.VRFs["default"]
	if !ok {
		return nil, fmt.Errorf("VRF 'default' not found")
	}

	instance, ok := vrf.ISISInstances["1"]
	if !ok {
		return nil, fmt.Errorf("ISIS instance '1' not found")
	}

	levelData, ok := instance.Level[fmt.Sprintf("%d", level)]
	if !ok {
		return nil, fmt.Errorf("ISIS level %d not found", level)
	}

	return levelData.LSPs, nil
}

// parseRouterFromLSP converts a JSON LSP into a Router struct.
func parseRouterFromLSP(lspID string, lsp jsonLSP, locator *Locator) Router {
	hostname := lsp.Hostname.Name
	if hostname == "" {
		hostname = lspID
	}

	// System ID: strip -00 suffix from LSP ID
	systemID := strings.Replace(lspID, "-00", "", 1)

	// Router ID and SR capabilities from routerCapabilities
	var routerID string
	var srgbBase, srgbRange, srlbBase, srlbRange, msd *int

	if len(lsp.RouterCapabilities) > 0 {
		cap := lsp.RouterCapabilities[0]
		routerID = cap.RouterID

		// SRGB
		if len(cap.SRCapabilities) > 0 {
			srCap := cap.SRCapabilities[0]
			srgbList := srCap.SRCapabilitySRGB
			if len(srgbList) == 0 {
				srgbList = srCap.SRGBRanges
			}
			if len(srgbList) > 0 {
				srgbBase = ptrInt(srgbList[0].SRGBBase)
				srgbRange = ptrInt(srgbList[0].SRGBRange)
			}
		}

		// SRLB
		if len(cap.SRLB.SRLBRanges) > 0 {
			srlbBase = ptrInt(cap.SRLB.SRLBRanges[0].SRLBBase)
			srlbRange = ptrInt(cap.SRLB.SRLBRanges[0].SRLBRange)
		}

		// MSD
		if cap.MSD.BaseMPLSImposition > 0 {
			msd = ptrInt(cap.MSD.BaseMPLSImposition)
		}
	}

	// Area
	var area string
	if len(lsp.AreaAddresses) > 0 {
		area = lsp.AreaAddresses[0].Address
	}

	// Interfaces
	interfaces := make([]string, 0, len(lsp.InterfaceAddresses))
	for _, addr := range lsp.InterfaceAddresses {
		if addr.IPv4Address != "" {
			interfaces = append(interfaces, addr.IPv4Address)
		}
	}

	// Neighbors
	neighbors := make([]Neighbor, 0, len(lsp.Neighbors))
	for _, n := range lsp.Neighbors {
		// Strip .00 suffix from neighbor system ID
		neighborHostname := strings.Replace(n.SystemID, ".00", "", 1)
		adjSIDs := make([]int, 0, len(n.AdjSIDs))
		for _, a := range n.AdjSIDs {
			adjSIDs = append(adjSIDs, a.AdjSID)
		}
		neighbors = append(neighbors, Neighbor{
			Hostname:     neighborHostname,
			Metric:       n.Metric,
			NeighborAddr: n.NeighborAddr,
			AdjSIDs:      adjSIDs,
		})
	}

	// Reachabilities and Node SID
	reachabilities := make([]Reachability, 0, len(lsp.Reachabilities))
	var nodeSID *int
	var nodeSIDPrefix string

	for _, r := range lsp.Reachabilities {
		prefix := fmt.Sprintf("%s/%d", r.ReachabilityV4Addr, r.MaskLength)

		var srInfo *SRInfo
		for _, sr := range r.SRPrefixReachabilities {
			if sr.Options.NodeSID {
				nodeSID = ptrInt(sr.SID)
				nodeSIDPrefix = prefix
				srInfo = &SRInfo{SID: sr.SID, IsNodeSID: true}
				break
			} else if sr.SID != 0 {
				srInfo = &SRInfo{SID: sr.SID, IsNodeSID: false}
			}
		}

		reachabilities = append(reachabilities, Reachability{
			Prefix: prefix,
			Metric: r.Metric,
			SR:     srInfo,
		})
	}

	// Compute SRGB/SRLB end values
	var srgbEnd, srlbEnd *int
	if srgbBase != nil && srgbRange != nil {
		srgbEnd = ptrInt(*srgbBase + *srgbRange - 1)
	}
	if srlbBase != nil && srlbRange != nil {
		srlbEnd = ptrInt(*srlbBase + *srlbRange - 1)
	}

	return Router{
		Hostname:       hostname,
		RouterID:       routerID,
		SystemID:       systemID,
		RouterType:     lsp.IntermediateSystem,
		Area:           area,
		IsOverloaded:   lsp.Flags.DBOverload,
		Sequence:       lsp.Sequence,
		Interfaces:     interfaces,
		Neighbors:      neighbors,
		Reachabilities: reachabilities,
		SRGBBase:       srgbBase,
		SRGBRange:      srgbRange,
		SRGBEnd:        srgbEnd,
		SRLBBase:       srlbBase,
		SRLBRange:      srlbRange,
		SRLBEnd:        srlbEnd,
		MSD:            msd,
		NodeSID:        nodeSID,
		NodeSIDPrefix:  nodeSIDPrefix,
		Location:       locator.Infer(hostname),
	}
}

func ptrInt(v int) *int {
	return &v
}
