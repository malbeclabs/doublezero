package isis

import (
	"encoding/json"
	"fmt"
)

// jsonDump represents the top-level JSON structure of an IS-IS dump.
type jsonDump struct {
	VRFs map[string]jsonVRF `json:"vrfs"`
}

// jsonVRF represents a VRF containing IS-IS instances.
type jsonVRF struct {
	ISISInstances map[string]jsonISISInstance `json:"isisInstances"`
}

// jsonISISInstance represents an IS-IS instance with levels.
type jsonISISInstance struct {
	Level map[string]jsonLevel `json:"level"`
}

// jsonLevel represents an IS-IS level (typically level 2) with LSPs.
type jsonLevel struct {
	LSPs map[string]jsonLSP `json:"lsps"`
}

// jsonLSP represents a Link State PDU from a router.
type jsonLSP struct {
	Hostname           jsonHostname           `json:"hostname"`
	Neighbors          []jsonNeighbor         `json:"neighbors"`
	RouterCapabilities jsonRouterCapabilities `json:"routerCapabilities"`
}

// jsonHostname contains the router hostname.
type jsonHostname struct {
	Name string `json:"name"`
}

// jsonNeighbor represents an IS-IS adjacency.
type jsonNeighbor struct {
	SystemID     string   `json:"systemId"`
	Metric       uint32   `json:"metric"`
	NeighborAddr string   `json:"neighborAddr"`
	AdjSIDs      []uint32 `json:"adjSids"`
}

// jsonRouterCapabilities contains router capability information.
type jsonRouterCapabilities struct {
	RouterID  string `json:"routerId"`
	SRGBBase  uint32 `json:"srgbBase"`
	SRGBRange uint32 `json:"srgbRange"`
}

// Parse parses raw IS-IS JSON data into a slice of LSPs.
func Parse(data []byte) ([]LSP, error) {
	var dump jsonDump
	if err := json.Unmarshal(data, &dump); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	var lsps []LSP

	// Navigate: vrfs.default.isisInstances.1.level.2.lsps
	defaultVRF, ok := dump.VRFs["default"]
	if !ok {
		return nil, fmt.Errorf("VRF 'default' not found")
	}

	instance, ok := defaultVRF.ISISInstances["1"]
	if !ok {
		return nil, fmt.Errorf("IS-IS instance '1' not found")
	}

	level2, ok := instance.Level["2"]
	if !ok {
		return nil, fmt.Errorf("IS-IS level '2' not found")
	}

	// Process each LSP
	for systemID, jsonLSP := range level2.LSPs {
		lsp := LSP{
			SystemID: systemID,
			Hostname: jsonLSP.Hostname.Name,
			RouterID: jsonLSP.RouterCapabilities.RouterID,
		}

		// Convert neighbors
		for _, jn := range jsonLSP.Neighbors {
			neighbor := Neighbor{
				SystemID:     jn.SystemID,
				Metric:       jn.Metric,
				NeighborAddr: jn.NeighborAddr,
				AdjSIDs:      jn.AdjSIDs,
			}
			lsp.Neighbors = append(lsp.Neighbors, neighbor)
		}

		lsps = append(lsps, lsp)
	}

	return lsps, nil
}
