package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/handlers/dberror"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// ISISNode represents a device node in the ISIS topology graph
type ISISNode struct {
	Data ISISNodeData `json:"data"`
}

type ISISNodeData struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
	MetroPK    string `json:"metroPK,omitempty"`
	SystemID   string `json:"systemId,omitempty"`
	RouterID   string `json:"routerId,omitempty"`
}

// ISISEdge represents an adjacency edge in the ISIS topology graph
type ISISEdge struct {
	Data ISISEdgeData `json:"data"`
}

type ISISEdgeData struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	Target       string   `json:"target"`
	Metric       uint32   `json:"metric,omitempty"`
	AdjSIDs      []uint32 `json:"adjSids,omitempty"`
	NeighborAddr string   `json:"neighborAddr,omitempty"`
}

// ISISTopologyResponse is the response for the ISIS topology endpoint
type ISISTopologyResponse struct {
	Nodes []ISISNode `json:"nodes"`
	Edges []ISISEdge `json:"edges"`
	Error string     `json:"error,omitempty"`
}

// GetISISTopology returns the full ISIS topology graph
func GetISISTopology(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()

	response := ISISTopologyResponse{
		Nodes: []ISISNode{},
		Edges: []ISISEdge{},
	}

	// Helper to run Neo4j query with retry
	runNeo4jQuery := func(cypher string) ([]*neo4jdriver.Record, error) {
		cfg := dberror.DefaultRetryConfig()
		return dberror.Retry(ctx, cfg, func() ([]*neo4jdriver.Record, error) {
			session := config.Neo4jSession(ctx)
			defer session.Close(ctx)

			result, err := session.Run(ctx, cypher, nil)
			if err != nil {
				return nil, err
			}
			return result.Collect(ctx)
		})
	}

	// Get devices with ISIS data
	deviceCypher := `
		MATCH (d:Device)
		WHERE d.isis_system_id IS NOT NULL
		OPTIONAL MATCH (d)-[:LOCATED_IN]->(m:Metro)
		RETURN d.pk AS pk,
		       d.code AS code,
		       d.status AS status,
		       d.device_type AS device_type,
		       d.isis_system_id AS system_id,
		       d.isis_router_id AS router_id,
		       m.pk AS metro_pk
	`

	deviceRecords, err := runNeo4jQuery(deviceCypher)
	if err != nil {
		log.Printf("ISIS topology device query error: %v", err)
		response.Error = dberror.UserMessage(err)
		writeJSON(w, response)
		return
	}

	for _, record := range deviceRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		status, _ := record.Get("status")
		deviceType, _ := record.Get("device_type")
		systemID, _ := record.Get("system_id")
		routerID, _ := record.Get("router_id")
		metroPK, _ := record.Get("metro_pk")

		response.Nodes = append(response.Nodes, ISISNode{
			Data: ISISNodeData{
				ID:         asString(pk),
				Label:      asString(code),
				Status:     asString(status),
				DeviceType: asString(deviceType),
				SystemID:   asString(systemID),
				RouterID:   asString(routerID),
				MetroPK:    asString(metroPK),
			},
		})
	}

	// Get all ISIS adjacencies
	adjCypher := `
		MATCH (from:Device)-[r:ISIS_ADJACENT]->(to:Device)
		RETURN from.pk AS from_pk,
		       to.pk AS to_pk,
		       r.metric AS metric,
		       r.neighbor_addr AS neighbor_addr,
		       r.adj_sids AS adj_sids
	`

	adjRecords, err := runNeo4jQuery(adjCypher)
	if err != nil {
		log.Printf("ISIS topology adjacency query error: %v", err)
		response.Error = dberror.UserMessage(err)
		writeJSON(w, response)
		return
	}

	for _, record := range adjRecords {
		fromPK, _ := record.Get("from_pk")
		toPK, _ := record.Get("to_pk")
		metric, _ := record.Get("metric")
		neighborAddr, _ := record.Get("neighbor_addr")
		adjSids, _ := record.Get("adj_sids")

		response.Edges = append(response.Edges, ISISEdge{
			Data: ISISEdgeData{
				ID:           asString(fromPK) + "->" + asString(toPK),
				Source:       asString(fromPK),
				Target:       asString(toPK),
				Metric:       uint32(asInt64(metric)),
				NeighborAddr: asString(neighborAddr),
				AdjSIDs:      asUint32Slice(adjSids),
			},
		})
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil) // Reuse existing metric for now

	writeJSON(w, response)
}

// Helper functions

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func asInt64(v any) int64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func asFloat64(v any) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0
	}
}

func asUint32Slice(v any) []uint32 {
	if v == nil {
		return nil
	}
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	result := make([]uint32, 0, len(arr))
	for _, item := range arr {
		result = append(result, uint32(asInt64(item)))
	}
	return result
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// PathHop represents a hop in a path
type PathHop struct {
	DevicePK   string `json:"devicePK"`
	DeviceCode string `json:"deviceCode"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
}

// PathResponse is the response for the path endpoint
type PathResponse struct {
	Path        []PathHop `json:"path"`
	TotalMetric uint32    `json:"totalMetric"`
	HopCount    int       `json:"hopCount"`
	Error       string    `json:"error,omitempty"`
}

// GetISISPath finds the shortest path between two devices using ISIS metrics
func GetISISPath(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	fromPK := r.URL.Query().Get("from")
	toPK := r.URL.Query().Get("to")
	mode := r.URL.Query().Get("mode") // "hops" or "latency"

	if fromPK == "" || toPK == "" {
		writeJSON(w, PathResponse{Error: "from and to parameters are required"})
		return
	}

	if fromPK == toPK {
		writeJSON(w, PathResponse{Error: "from and to must be different devices"})
		return
	}

	if mode == "" {
		mode = "hops" // default to fewest hops
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	var cypher string
	if mode == "latency" {
		// Use APOC Dijkstra for weighted shortest path (lowest total metric)
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			CALL apoc.algo.dijkstra(a, b, 'ISIS_ADJACENT>', 'metric') YIELD path, weight
			RETURN [n IN nodes(path) | {
				pk: n.pk,
				code: n.code,
				status: n.status,
				device_type: n.device_type
			}] AS devices,
			weight AS total_metric
		`
	} else {
		// Default: fewest hops using shortestPath
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			MATCH path = shortestPath((a)-[:ISIS_ADJACENT*]->(b))
			WITH path, reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS total_metric
			RETURN [n IN nodes(path) | {
				pk: n.pk,
				code: n.code,
				status: n.status,
				device_type: n.device_type
			}] AS devices,
			total_metric
		`
	}

	result, err := session.Run(ctx, cypher, map[string]any{
		"from_pk": fromPK,
		"to_pk":   toPK,
	})
	if err != nil {
		log.Printf("ISIS path query error: %v", err)
		writeJSON(w, PathResponse{Error: "Failed to find path: " + err.Error()})
		return
	}

	record, err := result.Single(ctx)
	if err != nil {
		log.Printf("ISIS path no result: %v", err)
		writeJSON(w, PathResponse{Error: "No path found between devices"})
		return
	}

	devicesVal, _ := record.Get("devices")
	totalMetric, _ := record.Get("total_metric")

	path := parsePathHops(devicesVal)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	writeJSON(w, PathResponse{
		Path:        path,
		TotalMetric: uint32(asInt64(totalMetric)),
		HopCount:    len(path) - 1,
	})
}

func parsePathHops(v any) []PathHop {
	if v == nil {
		return []PathHop{}
	}
	arr, ok := v.([]any)
	if !ok {
		return []PathHop{}
	}
	hops := make([]PathHop, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		hops = append(hops, PathHop{
			DevicePK:   asString(m["pk"]),
			DeviceCode: asString(m["code"]),
			Status:     asString(m["status"]),
			DeviceType: asString(m["device_type"]),
		})
	}
	return hops
}

// TopologyDiscrepancy represents a mismatch between configured and ISIS topology
type TopologyDiscrepancy struct {
	Type            string `json:"type"` // "missing_isis", "extra_isis", "metric_mismatch"
	LinkPK          string `json:"linkPK,omitempty"`
	LinkCode        string `json:"linkCode,omitempty"`
	DeviceAPK       string `json:"deviceAPK"`
	DeviceACode     string `json:"deviceACode"`
	DeviceBPK       string `json:"deviceBPK"`
	DeviceBCode     string `json:"deviceBCode"`
	ConfiguredRTTUs uint64 `json:"configuredRttUs,omitempty"`
	ISISMetric      uint32 `json:"isisMetric,omitempty"`
	Details         string `json:"details"`
}

// TopologyCompareResponse is the response for the topology compare endpoint
type TopologyCompareResponse struct {
	ConfiguredLinks int                   `json:"configuredLinks"`
	ISISAdjacencies int                   `json:"isisAdjacencies"`
	MatchedLinks    int                   `json:"matchedLinks"`
	Discrepancies   []TopologyDiscrepancy `json:"discrepancies"`
	Error           string                `json:"error,omitempty"`
}

// GetTopologyCompare compares configured links vs ISIS adjacencies
func GetTopologyCompare(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := TopologyCompareResponse{
		Discrepancies: []TopologyDiscrepancy{},
	}

	// Query 1: Find configured links and check if they have ISIS adjacencies
	configuredCypher := `
		MATCH (l:Link)-[:CONNECTS]->(da:Device)
		MATCH (l)-[:CONNECTS]->(db:Device)
		WHERE da.pk < db.pk
		OPTIONAL MATCH (da)-[isis:ISIS_ADJACENT]->(db)
		OPTIONAL MATCH (db)-[isis_rev:ISIS_ADJACENT]->(da)
		RETURN l.pk AS link_pk,
		       l.code AS link_code,
		       l.status AS link_status,
		       l.committed_rtt_ns AS configured_rtt_ns,
		       da.pk AS device_a_pk,
		       da.code AS device_a_code,
		       db.pk AS device_b_pk,
		       db.code AS device_b_code,
		       isis.metric AS isis_metric_forward,
		       isis IS NOT NULL AS has_forward_adj,
		       isis_rev IS NOT NULL AS has_reverse_adj
	`

	configuredResult, err := session.Run(ctx, configuredCypher, nil)
	if err != nil {
		log.Printf("Topology compare configured query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	configuredRecords, err := configuredResult.Collect(ctx)
	if err != nil {
		log.Printf("Topology compare configured collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	response.ConfiguredLinks = len(configuredRecords)

	for _, record := range configuredRecords {
		linkPK, _ := record.Get("link_pk")
		linkCode, _ := record.Get("link_code")
		linkStatus, _ := record.Get("link_status")
		configuredRTTNs, _ := record.Get("configured_rtt_ns")
		deviceAPK, _ := record.Get("device_a_pk")
		deviceACode, _ := record.Get("device_a_code")
		deviceBPK, _ := record.Get("device_b_pk")
		deviceBCode, _ := record.Get("device_b_code")
		hasForwardAdj, _ := record.Get("has_forward_adj")
		hasReverseAdj, _ := record.Get("has_reverse_adj")
		isisMetricForward, _ := record.Get("isis_metric_forward")

		hasForward := asBool(hasForwardAdj)
		hasReverse := asBool(hasReverseAdj)
		status := asString(linkStatus)

		if hasForward || hasReverse {
			response.MatchedLinks++
		}

		// Check for missing ISIS adjacencies on active links
		if status == "active" && !hasForward && !hasReverse {
			response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
				Type:        "missing_isis",
				LinkPK:      asString(linkPK),
				LinkCode:    asString(linkCode),
				DeviceAPK:   asString(deviceAPK),
				DeviceACode: asString(deviceACode),
				DeviceBPK:   asString(deviceBPK),
				DeviceBCode: asString(deviceBCode),
				Details:     "Active link has no ISIS adjacency in either direction",
			})
		} else if status == "active" && hasForward != hasReverse {
			direction := "forward only"
			if hasReverse && !hasForward {
				direction = "reverse only"
			}
			response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
				Type:        "missing_isis",
				LinkPK:      asString(linkPK),
				LinkCode:    asString(linkCode),
				DeviceAPK:   asString(deviceAPK),
				DeviceACode: asString(deviceACode),
				DeviceBPK:   asString(deviceBPK),
				DeviceBCode: asString(deviceBCode),
				Details:     "ISIS adjacency is " + direction + " (should be bidirectional)",
			})
		}

		// Check for metric mismatch
		configRTTNs := asInt64(configuredRTTNs)
		isisMetric := asInt64(isisMetricForward)
		if hasForward && configRTTNs > 0 && isisMetric > 0 {
			configRTTUs := uint64(configRTTNs) / 1000
			if configRTTUs > 0 {
				ratio := float64(isisMetric) / float64(configRTTUs)
				if ratio < 0.5 || ratio > 2.0 {
					response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
						Type:            "metric_mismatch",
						LinkPK:          asString(linkPK),
						LinkCode:        asString(linkCode),
						DeviceAPK:       asString(deviceAPK),
						DeviceACode:     asString(deviceACode),
						DeviceBPK:       asString(deviceBPK),
						DeviceBCode:     asString(deviceBCode),
						ConfiguredRTTUs: configRTTUs,
						ISISMetric:      uint32(isisMetric),
						Details:         "ISIS metric differs significantly from configured RTT",
					})
				}
			}
		}
	}

	// Query 2: Find ISIS adjacencies that don't correspond to any configured link
	extraCypher := `
		MATCH (da:Device)-[isis:ISIS_ADJACENT]->(db:Device)
		WHERE NOT EXISTS {
			MATCH (l:Link)-[:CONNECTS]->(da)
			MATCH (l)-[:CONNECTS]->(db)
		}
		RETURN da.pk AS device_a_pk,
		       da.code AS device_a_code,
		       db.pk AS device_b_pk,
		       db.code AS device_b_code,
		       isis.metric AS isis_metric,
		       isis.neighbor_addr AS neighbor_addr
	`

	extraResult, err := session.Run(ctx, extraCypher, nil)
	if err != nil {
		log.Printf("Topology compare extra query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	extraRecords, err := extraResult.Collect(ctx)
	if err != nil {
		log.Printf("Topology compare extra collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range extraRecords {
		deviceAPK, _ := record.Get("device_a_pk")
		deviceACode, _ := record.Get("device_a_code")
		deviceBPK, _ := record.Get("device_b_pk")
		deviceBCode, _ := record.Get("device_b_code")
		isisMetric, _ := record.Get("isis_metric")
		neighborAddr, _ := record.Get("neighbor_addr")

		response.Discrepancies = append(response.Discrepancies, TopologyDiscrepancy{
			Type:        "extra_isis",
			DeviceAPK:   asString(deviceAPK),
			DeviceACode: asString(deviceACode),
			DeviceBPK:   asString(deviceBPK),
			DeviceBCode: asString(deviceBCode),
			ISISMetric:  uint32(asInt64(isisMetric)),
			Details:     "ISIS adjacency exists (neighbor: " + asString(neighborAddr) + ") but no configured link found",
		})
	}

	// Count total ISIS adjacencies
	countCypher := `MATCH ()-[r:ISIS_ADJACENT]->() RETURN count(r) AS count`
	countResult, err := session.Run(ctx, countCypher, nil)
	if err != nil {
		log.Printf("Topology compare count query error: %v", err)
	} else {
		if countRecord, err := countResult.Single(ctx); err == nil {
			count, _ := countRecord.Get("count")
			response.ISISAdjacencies = int(asInt64(count))
		}
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	writeJSON(w, response)
}

func asBool(v any) bool {
	if v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

// ImpactDevice represents a device that would be affected by a failure
type ImpactDevice struct {
	PK         string `json:"pk"`
	Code       string `json:"code"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
}

// MetroImpact represents the impact of a device failure on a metro
type MetroImpact struct {
	PK               string `json:"pk"`
	Code             string `json:"code"`
	Name             string `json:"name"`
	TotalDevices     int    `json:"totalDevices"`     // Total ISIS devices in this metro
	RemainingDevices int    `json:"remainingDevices"` // Devices still reachable after failure
	IsolatedDevices  int    `json:"isolatedDevices"`  // Devices that become unreachable
}

// FailureImpactPath represents a path affected by device failure
type FailureImpactPath struct {
	FromPK       string `json:"fromPK"`
	FromCode     string `json:"fromCode"`
	ToPK         string `json:"toPK"`
	ToCode       string `json:"toCode"`
	BeforeHops   int    `json:"beforeHops"`
	BeforeMetric uint32 `json:"beforeMetric"`
	AfterHops    int    `json:"afterHops,omitempty"`   // 0 if no alternate path
	AfterMetric  uint32 `json:"afterMetric,omitempty"` // 0 if no alternate path
	HasAlternate bool   `json:"hasAlternate"`
}

// FailureImpactResponse is the response for the failure impact endpoint
type FailureImpactResponse struct {
	DevicePK           string              `json:"devicePK"`
	DeviceCode         string              `json:"deviceCode"`
	UnreachableDevices []ImpactDevice      `json:"unreachableDevices"`
	UnreachableCount   int                 `json:"unreachableCount"`
	AffectedPaths      []FailureImpactPath `json:"affectedPaths"`
	AffectedPathCount  int                 `json:"affectedPathCount"`
	MetroImpact        []MetroImpact       `json:"metroImpact"`
	Error              string              `json:"error,omitempty"`
}

// GetFailureImpact returns devices that would become unreachable if a device goes down
func GetFailureImpact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Get device PK from URL path
	devicePK := r.PathValue("pk")
	if devicePK == "" {
		writeJSON(w, FailureImpactResponse{Error: "device pk is required"})
		return
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := FailureImpactResponse{
		DevicePK:           devicePK,
		UnreachableDevices: []ImpactDevice{},
		AffectedPaths:      []FailureImpactPath{},
		MetroImpact:        []MetroImpact{},
	}

	// First get the device code
	deviceCypher := `MATCH (d:Device {pk: $pk}) RETURN d.code AS code`
	deviceResult, err := session.Run(ctx, deviceCypher, map[string]any{"pk": devicePK})
	if err != nil {
		log.Printf("Failure impact device query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}
	if deviceRecord, err := deviceResult.Single(ctx); err == nil {
		code, _ := deviceRecord.Get("code")
		response.DeviceCode = asString(code)
	}

	// Find devices that would become unreachable if this device goes down
	// Strategy: Find a reference device (most connected, not the target), then find all devices
	// reachable from it without going through the target. Unreachable = ISIS devices not in that set.
	impactCypher := `
		// First, find a good reference device (most ISIS adjacencies, not the target)
		MATCH (ref:Device)-[:ISIS_ADJACENT]-()
		WHERE ref.pk <> $device_pk AND ref.isis_system_id IS NOT NULL
		WITH ref, count(*) AS adjCount
		ORDER BY adjCount DESC
		LIMIT 1

		// Find all devices reachable from reference without going through target
		CALL {
			WITH ref
			MATCH (target:Device {pk: $device_pk})
			MATCH path = (ref)-[:ISIS_ADJACENT*0..20]-(reachable:Device)
			WHERE reachable.isis_system_id IS NOT NULL
			  AND NONE(n IN nodes(path) WHERE n.pk = $device_pk)
			RETURN DISTINCT reachable
		}

		// Find all ISIS devices
		WITH collect(reachable.pk) AS reachablePKs
		MATCH (d:Device)
		WHERE d.isis_system_id IS NOT NULL
		  AND d.pk <> $device_pk
		  AND NOT d.pk IN reachablePKs
		RETURN d.pk AS pk,
		       d.code AS code,
		       d.status AS status,
		       d.device_type AS device_type
	`

	impactResult, err := session.Run(ctx, impactCypher, map[string]any{
		"device_pk": devicePK,
	})
	if err != nil {
		log.Printf("Failure impact query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	impactRecords, err := impactResult.Collect(ctx)
	if err != nil {
		log.Printf("Failure impact collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range impactRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		status, _ := record.Get("status")
		deviceType, _ := record.Get("device_type")

		response.UnreachableDevices = append(response.UnreachableDevices, ImpactDevice{
			PK:         asString(pk),
			Code:       asString(code),
			Status:     asString(status),
			DeviceType: asString(deviceType),
		})
	}

	response.UnreachableCount = len(response.UnreachableDevices)

	// Calculate metro-level impact
	// Build a set of unreachable device PKs for quick lookup
	unreachablePKs := make(map[string]bool)
	for _, device := range response.UnreachableDevices {
		unreachablePKs[device.PK] = true
	}
	// The failing device itself also counts as unavailable
	unreachablePKs[devicePK] = true

	// Query all metros with ISIS devices and their device counts
	metroCypher := `
		MATCH (m:Metro)<-[:LOCATED_IN]-(d:Device)
		WHERE d.isis_system_id IS NOT NULL
		RETURN m.pk AS metro_pk,
		       m.code AS metro_code,
		       m.name AS metro_name,
		       collect(d.pk) AS device_pks
	`
	metroResult, err := session.Run(ctx, metroCypher, map[string]any{})
	if err != nil {
		log.Printf("Failure impact metro query error: %v", err)
		// Don't fail the whole response, just log the error
	} else {
		metroRecords, err := metroResult.Collect(ctx)
		if err != nil {
			log.Printf("Failure impact metro collect error: %v", err)
		} else {
			for _, record := range metroRecords {
				metroPK, _ := record.Get("metro_pk")
				metroCode, _ := record.Get("metro_code")
				metroName, _ := record.Get("metro_name")
				devicePKsRaw, _ := record.Get("device_pks")

				devicePKsList, ok := devicePKsRaw.([]any)
				if !ok {
					continue
				}

				totalDevices := len(devicePKsList)
				isolatedCount := 0
				for _, pk := range devicePKsList {
					if unreachablePKs[asString(pk)] {
						isolatedCount++
					}
				}

				// Only include metros where at least one device is affected
				if isolatedCount > 0 {
					response.MetroImpact = append(response.MetroImpact, MetroImpact{
						PK:               asString(metroPK),
						Code:             asString(metroCode),
						Name:             asString(metroName),
						TotalDevices:     totalDevices,
						RemainingDevices: totalDevices - isolatedCount,
						IsolatedDevices:  isolatedCount,
					})
				}
			}
		}
	}

	// Find affected paths - paths that currently use this device as an intermediate hop
	// For each affected path, calculate before (through device) and after (rerouted) metrics
	affectedCypher := `
		// Get the failing device
		MATCH (target:Device {pk: $device_pk})
		WHERE target.isis_system_id IS NOT NULL

		// Find devices that have shortest paths going through target
		// These are neighbors of target where going through target is part of their shortest path
		MATCH (target)-[r1:ISIS_ADJACENT]-(neighbor1:Device)
		WHERE neighbor1.isis_system_id IS NOT NULL
		WITH target, neighbor1, r1.metric AS metric1
		MATCH (target)-[r2:ISIS_ADJACENT]-(neighbor2:Device)
		WHERE neighbor2.isis_system_id IS NOT NULL
		  AND neighbor2.pk > neighbor1.pk  // Avoid duplicate pairs
		WITH target, neighbor1, neighbor2, metric1, r2.metric AS metric2

		// Calculate path through target device
		WITH neighbor1, neighbor2, target,
		     metric1 + metric2 AS throughTargetMetric

		// Find shortest path between neighbors NOT going through target
		OPTIONAL MATCH altPath = shortestPath((neighbor1)-[:ISIS_ADJACENT*]-(neighbor2))
		WHERE NONE(n IN nodes(altPath) WHERE n.pk = target.pk)
		WITH neighbor1, neighbor2, target, throughTargetMetric,
		     CASE WHEN altPath IS NOT NULL THEN length(altPath) ELSE 0 END AS altHops,
		     CASE WHEN altPath IS NOT NULL
		          THEN reduce(total = 0, rel IN relationships(altPath) | total + coalesce(rel.metric, 0))
		          ELSE 0 END AS altMetric

		// Only include paths where alternate exists and is worse or doesn't exist
		WHERE altHops > 0 OR altMetric = 0
		RETURN neighbor1.pk AS from_pk,
		       neighbor1.code AS from_code,
		       neighbor2.pk AS to_pk,
		       neighbor2.code AS to_code,
		       2 AS before_hops,
		       throughTargetMetric AS before_metric,
		       altHops AS after_hops,
		       altMetric AS after_metric,
		       altHops > 0 AS has_alternate
		ORDER BY (altMetric - throughTargetMetric) DESC
		LIMIT 20
	`

	affectedResult, err := session.Run(ctx, affectedCypher, map[string]any{
		"device_pk": devicePK,
	})
	if err != nil {
		log.Printf("Failure impact affected paths query error: %v", err)
		// Don't fail the whole response, just log the error
	} else {
		affectedRecords, err := affectedResult.Collect(ctx)
		if err != nil {
			log.Printf("Failure impact affected paths collect error: %v", err)
		} else {
			for _, record := range affectedRecords {
				fromPK, _ := record.Get("from_pk")
				fromCode, _ := record.Get("from_code")
				toPK, _ := record.Get("to_pk")
				toCode, _ := record.Get("to_code")
				beforeHops, _ := record.Get("before_hops")
				beforeMetric, _ := record.Get("before_metric")
				afterHops, _ := record.Get("after_hops")
				afterMetric, _ := record.Get("after_metric")
				hasAlternate, _ := record.Get("has_alternate")

				response.AffectedPaths = append(response.AffectedPaths, FailureImpactPath{
					FromPK:       asString(fromPK),
					FromCode:     asString(fromCode),
					ToPK:         asString(toPK),
					ToCode:       asString(toCode),
					BeforeHops:   int(asInt64(beforeHops)),
					BeforeMetric: uint32(asInt64(beforeMetric)),
					AfterHops:    int(asInt64(afterHops)),
					AfterMetric:  uint32(asInt64(afterMetric)),
					HasAlternate: asBool(hasAlternate),
				})
			}
		}
	}
	response.AffectedPathCount = len(response.AffectedPaths)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Failure impact: %s, unreachable=%d, affectedPaths=%d, metrosImpacted=%d in %v",
		response.DeviceCode, response.UnreachableCount, response.AffectedPathCount, len(response.MetroImpact), duration)

	writeJSON(w, response)
}

// MultiPathHop represents a hop in a path with edge metric information
type MultiPathHop struct {
	DevicePK        string  `json:"devicePK"`
	DeviceCode      string  `json:"deviceCode"`
	Status          string  `json:"status"`
	DeviceType      string  `json:"deviceType"`
	EdgeMetric      uint32  `json:"edgeMetric,omitempty"`      // ISIS metric to reach this hop from previous
	EdgeMeasuredMs  float64 `json:"edgeMeasuredMs,omitempty"`  // measured RTT in ms to reach this hop
	EdgeJitterMs    float64 `json:"edgeJitterMs,omitempty"`    // measured jitter in ms
	EdgeLossPct     float64 `json:"edgeLossPct,omitempty"`     // packet loss percentage
	EdgeSampleCount int64   `json:"edgeSampleCount,omitempty"` // number of samples for confidence
}

// SinglePath represents one path in a multi-path response
type SinglePath struct {
	Path              []MultiPathHop `json:"path"`
	TotalMetric       uint32         `json:"totalMetric"`
	HopCount          int            `json:"hopCount"`
	MeasuredLatencyMs float64        `json:"measuredLatencyMs,omitempty"` // sum of measured RTT along path
	TotalSamples      int64          `json:"totalSamples,omitempty"`      // min samples across hops
}

// MultiPathResponse is the response for the K-shortest paths endpoint
type MultiPathResponse struct {
	Paths []SinglePath `json:"paths"`
	From  string       `json:"from"`
	To    string       `json:"to"`
	Error string       `json:"error,omitempty"`
}

// GetISISPaths finds K-shortest paths between two devices
// mode parameter: "hops" (default) sorts by hop count, "latency" sorts by measured latency
func GetISISPaths(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	fromPK := r.URL.Query().Get("from")
	toPK := r.URL.Query().Get("to")
	kStr := r.URL.Query().Get("k")
	pathMode := r.URL.Query().Get("mode") // "hops" or "latency"
	if pathMode == "" {
		pathMode = "hops"
	}

	if fromPK == "" || toPK == "" {
		writeJSON(w, MultiPathResponse{Error: "from and to parameters are required"})
		return
	}

	if fromPK == toPK {
		writeJSON(w, MultiPathResponse{Error: "from and to must be different devices"})
		return
	}

	k := 5 // default
	if kStr != "" {
		if parsed, err := strconv.Atoi(kStr); err == nil && parsed > 0 && parsed <= 10 {
			k = parsed
		}
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MultiPathResponse{
		From:  fromPK,
		To:    toPK,
		Paths: []SinglePath{},
	}

	var cypher string
	if pathMode == "latency" {
		// Latency mode: Use APOC Dijkstra to find lowest total metric path
		// This can find longer paths if they have lower total latency
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			CALL apoc.algo.dijkstra(a, b, 'ISIS_ADJACENT>', 'metric') YIELD path, weight
			WITH path, toInteger(weight) AS totalMetric
			WITH path, totalMetric,
			     [n IN nodes(path) | {
			       pk: n.pk,
			       code: n.code,
			       status: n.status,
			       device_type: n.device_type
			     }] AS nodeList,
			     [r IN relationships(path) | r.metric] AS edgeMetrics
			RETURN nodeList, edgeMetrics, totalMetric
			LIMIT 1
		`
	} else {
		// Hops mode: Use allShortestPaths to find minimum hop paths
		cypher = `
			MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
			CALL {
				WITH a, b
				MATCH path = allShortestPaths((a)-[:ISIS_ADJACENT*]->(b))
				RETURN path,
				       reduce(cost = 0, r IN relationships(path) | cost + coalesce(r.metric, 1)) AS totalMetric
			}
			WITH path, totalMetric
			ORDER BY totalMetric
			LIMIT 50
			WITH path, totalMetric,
			     [n IN nodes(path) | {
			       pk: n.pk,
			       code: n.code,
			       status: n.status,
			       device_type: n.device_type
			     }] AS nodeList,
			     [r IN relationships(path) | r.metric] AS edgeMetrics
			RETURN nodeList, edgeMetrics, totalMetric
		`
	}

	result, err := session.Run(ctx, cypher, map[string]any{
		"from_pk": fromPK,
		"to_pk":   toPK,
	})
	if err != nil {
		log.Printf("ISIS multi-path query error: %v", err)
		response.Error = "Failed to find paths: " + err.Error()
		writeJSON(w, response)
		return
	}

	records, err := result.Collect(ctx)
	if err != nil {
		log.Printf("ISIS multi-path collect error: %v", err)
		response.Error = "Failed to collect paths: " + err.Error()
		writeJSON(w, response)
		return
	}

	if len(records) == 0 {
		response.Error = "No paths found between devices"
		writeJSON(w, response)
		return
	}

	// Track unique paths to avoid duplicates
	seenPaths := make(map[string]bool)

	for _, record := range records {
		nodeListVal, _ := record.Get("nodeList")
		edgeMetricsVal, _ := record.Get("edgeMetrics")
		totalMetric, _ := record.Get("totalMetric")

		hops := parseNodeListWithMetrics(nodeListVal, edgeMetricsVal)
		if len(hops) == 0 {
			continue
		}

		// Create a key for deduplication based on the path's device PKs
		pathKey := ""
		for _, hop := range hops {
			pathKey += hop.DevicePK + ","
		}

		if seenPaths[pathKey] {
			continue
		}
		seenPaths[pathKey] = true

		response.Paths = append(response.Paths, SinglePath{
			Path:        hops,
			TotalMetric: uint32(asInt64(totalMetric)),
			HopCount:    len(hops) - 1,
		})

		if len(response.Paths) >= k {
			break
		}
	}

	// Enrich paths with measured latency from ClickHouse
	if err := enrichPathsWithMeasuredLatency(ctx, &response); err != nil {
		log.Printf("enrichPathsWithMeasuredLatency error: %v", err)
		response.Error = fmt.Sprintf("failed to enrich paths with measured latency: %v", err)
	}

	// Re-sort paths based on mode
	if pathMode == "latency" {
		// Sort by total measured latency, fall back to ISIS metric if no measured data
		slices.SortFunc(response.Paths, func(a, b SinglePath) int {
			// If both have measured latency, sort by that
			if a.MeasuredLatencyMs > 0 && b.MeasuredLatencyMs > 0 {
				if a.MeasuredLatencyMs < b.MeasuredLatencyMs {
					return -1
				}
				if a.MeasuredLatencyMs > b.MeasuredLatencyMs {
					return 1
				}
			}
			// Fall back to ISIS metric (which represents configured latency)
			if a.TotalMetric < b.TotalMetric {
				return -1
			}
			if a.TotalMetric > b.TotalMetric {
				return 1
			}
			return 0
		})
	} else {
		// Sort by hop count first, then by metric (default "hops" mode)
		slices.SortFunc(response.Paths, func(a, b SinglePath) int {
			if a.HopCount != b.HopCount {
				return a.HopCount - b.HopCount
			}
			if a.TotalMetric < b.TotalMetric {
				return -1
			}
			if a.TotalMetric > b.TotalMetric {
				return 1
			}
			return 0
		})
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)
	log.Printf("ISIS multi-path query (%s mode) returned %d paths in %v", pathMode, len(response.Paths), duration)

	writeJSON(w, response)
}

func parseNodeListWithMetrics(nodeListVal, edgeMetricsVal any) []MultiPathHop {
	if nodeListVal == nil {
		return []MultiPathHop{}
	}
	nodeArr, ok := nodeListVal.([]any)
	if !ok {
		return []MultiPathHop{}
	}

	// Parse edge metrics
	var edgeMetrics []int64
	if edgeMetricsVal != nil {
		if metricsArr, ok := edgeMetricsVal.([]any); ok {
			for _, m := range metricsArr {
				edgeMetrics = append(edgeMetrics, asInt64(m))
			}
		}
	}

	hops := make([]MultiPathHop, 0, len(nodeArr))
	for i, item := range nodeArr {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}

		hop := MultiPathHop{
			DevicePK:   asString(m["pk"]),
			DeviceCode: asString(m["code"]),
			Status:     asString(m["status"]),
			DeviceType: asString(m["device_type"]),
		}

		// Edge metric is the metric to reach this hop from the previous one
		// So hop[i] uses edgeMetrics[i-1]
		if i > 0 && i-1 < len(edgeMetrics) {
			hop.EdgeMetric = uint32(edgeMetrics[i-1])
		}

		hops = append(hops, hop)
	}
	return hops
}

// linkLatencyData holds measured latency data for a link
type linkLatencyData struct {
	SideAPK     string
	SideZPK     string
	AvgRttMs    float64
	AvgJitterMs float64
	LossPct     float64
	SampleCount uint64
}

// enrichPathsWithMeasuredLatency queries ClickHouse for measured latency and adds it to path hops
func enrichPathsWithMeasuredLatency(ctx context.Context, response *MultiPathResponse) error {
	if len(response.Paths) == 0 {
		return nil
	}

	// Query ClickHouse for measured latency per link, including device endpoints
	query := `
		SELECT
			l.side_a_pk,
			l.side_z_pk,
			round(avg(lat.rtt_us) / 1000.0, 3) AS avg_rtt_ms,
			round(avg(abs(lat.ipdv_us)) / 1000.0, 3) AS avg_jitter_ms,
			countIf(lat.loss OR lat.rtt_us = 0) * 100.0 / count(*) AS loss_pct,
			count(*) AS sample_count
		FROM dz_links_current l
		JOIN fact_dz_device_link_latency lat ON l.pk = lat.link_pk
		WHERE lat.event_ts > now() - INTERVAL 3 HOUR
		  AND l.side_a_pk != ''
		  AND l.side_z_pk != ''
		GROUP BY l.side_a_pk, l.side_z_pk
	`

	rows, err := config.DB.Query(ctx, query)
	if err != nil {
		return fmt.Errorf("enrichPathsWithMeasuredLatency query error: %w", err)
	}
	defer rows.Close()

	// Build lookup map: "deviceA:deviceB" -> latency data
	// Store both directions since links are bidirectional
	latencyMap := make(map[string]linkLatencyData)
	for rows.Next() {
		var data linkLatencyData
		if err := rows.Scan(&data.SideAPK, &data.SideZPK, &data.AvgRttMs, &data.AvgJitterMs, &data.LossPct, &data.SampleCount); err != nil {
			return fmt.Errorf("enrichPathsWithMeasuredLatency scan error: %w", err)
		}
		// Store in both directions
		latencyMap[data.SideAPK+":"+data.SideZPK] = data
		latencyMap[data.SideZPK+":"+data.SideAPK] = data
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("enrichPathsWithMeasuredLatency rows error: %w", err)
	}

	// Update each path with measured latency
	for pathIdx := range response.Paths {
		path := &response.Paths[pathIdx]
		var totalMeasuredMs float64
		var minSamples int64 = -1

		for hopIdx := 1; hopIdx < len(path.Path); hopIdx++ {
			prevDevice := path.Path[hopIdx-1].DevicePK
			currDevice := path.Path[hopIdx].DevicePK
			key := prevDevice + ":" + currDevice

			if data, ok := latencyMap[key]; ok {
				path.Path[hopIdx].EdgeMeasuredMs = data.AvgRttMs
				path.Path[hopIdx].EdgeJitterMs = data.AvgJitterMs
				path.Path[hopIdx].EdgeLossPct = data.LossPct
				path.Path[hopIdx].EdgeSampleCount = int64(data.SampleCount)
				totalMeasuredMs += data.AvgRttMs
				if minSamples < 0 || int64(data.SampleCount) < minSamples {
					minSamples = int64(data.SampleCount)
				}
			}
		}

		if totalMeasuredMs > 0 {
			path.MeasuredLatencyMs = totalMeasuredMs
		}
		if minSamples > 0 {
			path.TotalSamples = minSamples
		}
	}
	return nil
}

// CriticalLink represents a link that is critical for network connectivity
type CriticalLink struct {
	SourcePK   string `json:"sourcePK"`
	SourceCode string `json:"sourceCode"`
	TargetPK   string `json:"targetPK"`
	TargetCode string `json:"targetCode"`
	Metric     uint32 `json:"metric"`
	Criticality string `json:"criticality"` // "critical", "important", "redundant"
}

// CriticalLinksResponse is the response for the critical links endpoint
type CriticalLinksResponse struct {
	Links []CriticalLink `json:"links"`
	Error string         `json:"error,omitempty"`
}

// GetCriticalLinks returns links that are critical for network connectivity
// Critical links are identified based on node degrees and connectivity patterns
func GetCriticalLinks(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := CriticalLinksResponse{
		Links: []CriticalLink{},
	}

	// Efficient approach: for each edge, check the degree of both endpoints
	// - If either endpoint has degree 1, this is a critical link (leaf edge)
	// - If min(degreeA, degreeB) == 2, it's important (limited redundancy)
	// - Otherwise it's redundant (well-connected)
	cypher := `
		MATCH (a:Device)-[r:ISIS_ADJACENT]-(b:Device)
		WHERE a.isis_system_id IS NOT NULL
		  AND b.isis_system_id IS NOT NULL
		  AND id(a) < id(b)
		WITH a, b, min(r.metric) AS metric  // Deduplicate multiple edges between same nodes
		// Count neighbors for each endpoint
		OPTIONAL MATCH (a)-[:ISIS_ADJACENT]-(na:Device)
		WHERE na.isis_system_id IS NOT NULL
		WITH a, b, metric, count(DISTINCT na) AS degreeA
		OPTIONAL MATCH (b)-[:ISIS_ADJACENT]-(nb:Device)
		WHERE nb.isis_system_id IS NOT NULL
		WITH a, b, metric, degreeA, count(DISTINCT nb) AS degreeB
		RETURN a.pk AS sourcePK,
		       a.code AS sourceCode,
		       b.pk AS targetPK,
		       b.code AS targetCode,
		       metric,
		       degreeA,
		       degreeB
		ORDER BY CASE
		  WHEN degreeA = 1 OR degreeB = 1 THEN 0
		  WHEN degreeA = 2 OR degreeB = 2 THEN 1
		  ELSE 2
		END, metric DESC
	`

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		log.Printf("Critical links query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	records, err := result.Collect(ctx)
	if err != nil {
		log.Printf("Critical links collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range records {
		sourcePK, _ := record.Get("sourcePK")
		sourceCode, _ := record.Get("sourceCode")
		targetPK, _ := record.Get("targetPK")
		targetCode, _ := record.Get("targetCode")
		metric, _ := record.Get("metric")
		degreeA, _ := record.Get("degreeA")
		degreeB, _ := record.Get("degreeB")

		dA := asInt64(degreeA)
		dB := asInt64(degreeB)
		minDegree := dA
		if dB < dA {
			minDegree = dB
		}

		// Determine criticality based on minimum degree
		var criticality string
		if minDegree <= 1 {
			criticality = "critical" // At least one endpoint has only this connection
		} else if minDegree == 2 {
			criticality = "important" // Limited redundancy
		} else {
			criticality = "redundant" // Well-connected endpoints
		}

		response.Links = append(response.Links, CriticalLink{
			SourcePK:    asString(sourcePK),
			SourceCode:  asString(sourceCode),
			TargetPK:    asString(targetPK),
			TargetCode:  asString(targetCode),
			Metric:      uint32(asInt64(metric)),
			Criticality: criticality,
		})
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	criticalCount := 0
	importantCount := 0
	for _, link := range response.Links {
		if link.Criticality == "critical" {
			criticalCount++
		} else if link.Criticality == "important" {
			importantCount++
		}
	}
	log.Printf("Critical links query returned %d links (%d critical, %d important) in %v",
		len(response.Links), criticalCount, importantCount, duration)

	writeJSON(w, response)
}

// RedundancyIssue represents a single redundancy issue in the network
type RedundancyIssue struct {
	Type        string `json:"type"`        // "leaf_device", "critical_link", "single_exit_metro", "no_backup_device"
	Severity    string `json:"severity"`    // "critical", "warning", "info"
	EntityPK    string `json:"entityPK"`    // PK of affected entity
	EntityCode  string `json:"entityCode"`  // Code/name of affected entity
	EntityType  string `json:"entityType"`  // "device", "link", "metro"
	Description string `json:"description"` // Human-readable description
	Impact      string `json:"impact"`      // Impact description
	// Extra fields for links
	TargetPK   string `json:"targetPK,omitempty"`
	TargetCode string `json:"targetCode,omitempty"`
	// Extra fields for context
	MetroPK   string `json:"metroPK,omitempty"`
	MetroCode string `json:"metroCode,omitempty"`
}

// RedundancyReportResponse is the response for the redundancy report endpoint
type RedundancyReportResponse struct {
	Issues         []RedundancyIssue `json:"issues"`
	Summary        RedundancySummary `json:"summary"`
	Error          string            `json:"error,omitempty"`
}

type RedundancySummary struct {
	TotalIssues     int `json:"totalIssues"`
	CriticalCount   int `json:"criticalCount"`
	WarningCount    int `json:"warningCount"`
	InfoCount       int `json:"infoCount"`
	LeafDevices     int `json:"leafDevices"`
	CriticalLinks   int `json:"criticalLinks"`
	SingleExitMetros int `json:"singleExitMetros"`
}

// GetRedundancyReport returns a comprehensive redundancy analysis report
func GetRedundancyReport(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := RedundancyReportResponse{
		Issues: []RedundancyIssue{},
	}

	// 1. Find leaf devices (devices with only 1 ISIS neighbor)
	leafCypher := `
		MATCH (d:Device)
		WHERE d.isis_system_id IS NOT NULL
		OPTIONAL MATCH (d)-[:ISIS_ADJACENT]-(n:Device)
		WHERE n.isis_system_id IS NOT NULL
		WITH d, count(DISTINCT n) AS neighborCount
		WHERE neighborCount = 1
		OPTIONAL MATCH (d)-[:LOCATED_IN]->(m:Metro)
		RETURN d.pk AS pk,
		       d.code AS code,
		       m.pk AS metroPK,
		       m.code AS metroCode
		ORDER BY d.code
	`

	leafResult, err := session.Run(ctx, leafCypher, nil)
	if err != nil {
		log.Printf("Redundancy report leaf devices query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	leafRecords, err := leafResult.Collect(ctx)
	if err != nil {
		log.Printf("Redundancy report leaf devices collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range leafRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		metroPK, _ := record.Get("metroPK")
		metroCode, _ := record.Get("metroCode")

		response.Issues = append(response.Issues, RedundancyIssue{
			Type:        "leaf_device",
			Severity:    "critical",
			EntityPK:    asString(pk),
			EntityCode:  asString(code),
			EntityType:  "device",
			Description: "Device has only one ISIS neighbor",
			Impact:      "If the single neighbor fails, this device loses connectivity to the network",
			MetroPK:     asString(metroPK),
			MetroCode:   asString(metroCode),
		})
	}

	// 2. Find critical links (links where at least one endpoint has only this connection)
	criticalLinksCypher := `
		MATCH (a:Device)-[r:ISIS_ADJACENT]-(b:Device)
		WHERE a.isis_system_id IS NOT NULL
		  AND b.isis_system_id IS NOT NULL
		  AND id(a) < id(b)
		WITH a, b, min(r.metric) AS metric
		OPTIONAL MATCH (a)-[:ISIS_ADJACENT]-(na:Device)
		WHERE na.isis_system_id IS NOT NULL
		WITH a, b, metric, count(DISTINCT na) AS degreeA
		OPTIONAL MATCH (b)-[:ISIS_ADJACENT]-(nb:Device)
		WHERE nb.isis_system_id IS NOT NULL
		WITH a, b, metric, degreeA, count(DISTINCT nb) AS degreeB
		WHERE degreeA = 1 OR degreeB = 1
		RETURN a.pk AS sourcePK,
		       a.code AS sourceCode,
		       b.pk AS targetPK,
		       b.code AS targetCode,
		       degreeA,
		       degreeB
		ORDER BY sourceCode
	`

	criticalResult, err := session.Run(ctx, criticalLinksCypher, nil)
	if err != nil {
		log.Printf("Redundancy report critical links query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	criticalRecords, err := criticalResult.Collect(ctx)
	if err != nil {
		log.Printf("Redundancy report critical links collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range criticalRecords {
		sourcePK, _ := record.Get("sourcePK")
		sourceCode, _ := record.Get("sourceCode")
		targetPK, _ := record.Get("targetPK")
		targetCode, _ := record.Get("targetCode")

		response.Issues = append(response.Issues, RedundancyIssue{
			Type:        "critical_link",
			Severity:    "critical",
			EntityPK:    asString(sourcePK),
			EntityCode:  asString(sourceCode),
			EntityType:  "link",
			TargetPK:    asString(targetPK),
			TargetCode:  asString(targetCode),
			Description: "Link connects a leaf device to the network",
			Impact:      "If this link fails, one or both devices lose network connectivity",
		})
	}

	// 3. Find single-exit metros (metros where only one device has external connections)
	singleExitCypher := `
		MATCH (m:Metro)<-[:LOCATED_IN]-(d:Device)
		WHERE d.isis_system_id IS NOT NULL
		MATCH (d)-[:ISIS_ADJACENT]-(n:Device)
		WHERE n.isis_system_id IS NOT NULL
		OPTIONAL MATCH (n)-[:LOCATED_IN]->(nm:Metro)
		WITH m, d, n, nm
		WHERE nm IS NULL OR nm.pk <> m.pk
		WITH m, count(DISTINCT d) AS exitDeviceCount
		WHERE exitDeviceCount = 1
		RETURN m.pk AS pk,
		       m.code AS code,
		       m.name AS name
		ORDER BY m.code
	`

	singleExitResult, err := session.Run(ctx, singleExitCypher, nil)
	if err != nil {
		log.Printf("Redundancy report single-exit metros query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	singleExitRecords, err := singleExitResult.Collect(ctx)
	if err != nil {
		log.Printf("Redundancy report single-exit metros collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range singleExitRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		name, _ := record.Get("name")

		displayName := asString(name)
		if displayName == "" {
			displayName = asString(code)
		}

		response.Issues = append(response.Issues, RedundancyIssue{
			Type:        "single_exit_metro",
			Severity:    "warning",
			EntityPK:    asString(pk),
			EntityCode:  displayName,
			EntityType:  "metro",
			Description: "Metro has only one device with external connections",
			Impact:      "If that device fails, the entire metro loses external connectivity",
		})
	}

	// Build summary
	criticalCount := 0
	warningCount := 0
	infoCount := 0
	leafDeviceCount := 0
	criticalLinkCount := 0
	singleExitMetroCount := 0

	for _, issue := range response.Issues {
		switch issue.Severity {
		case "critical":
			criticalCount++
		case "warning":
			warningCount++
		case "info":
			infoCount++
		}

		switch issue.Type {
		case "leaf_device":
			leafDeviceCount++
		case "critical_link":
			criticalLinkCount++
		case "single_exit_metro":
			singleExitMetroCount++
		}
	}

	response.Summary = RedundancySummary{
		TotalIssues:      len(response.Issues),
		CriticalCount:    criticalCount,
		WarningCount:     warningCount,
		InfoCount:        infoCount,
		LeafDevices:      leafDeviceCount,
		CriticalLinks:    criticalLinkCount,
		SingleExitMetros: singleExitMetroCount,
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Redundancy report returned %d issues (%d critical, %d warning, %d info) in %v",
		len(response.Issues), criticalCount, warningCount, infoCount, duration)

	writeJSON(w, response)
}

// MetroConnectivity represents connectivity between two metros
type MetroConnectivity struct {
	FromMetroPK      string  `json:"fromMetroPK"`
	FromMetroCode    string  `json:"fromMetroCode"`
	FromMetroName    string  `json:"fromMetroName"`
	ToMetroPK        string  `json:"toMetroPK"`
	ToMetroCode      string  `json:"toMetroCode"`
	ToMetroName      string  `json:"toMetroName"`
	PathCount        int     `json:"pathCount"`
	MinHops          int     `json:"minHops"`
	MinMetric        int64   `json:"minMetric"`
	BottleneckBwGbps float64 `json:"bottleneckBwGbps,omitempty"` // min bandwidth along best path
}

// MetroConnectivityResponse is the response for the metro connectivity endpoint
type MetroConnectivityResponse struct {
	Metros       []MetroInfo         `json:"metros"`
	Connectivity []MetroConnectivity `json:"connectivity"`
	Error        string              `json:"error,omitempty"`
}

// MetroInfo is a lightweight metro representation for the matrix
type MetroInfo struct {
	PK   string `json:"pk"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// GetMetroConnectivity returns the connectivity matrix between all metros
func GetMetroConnectivity(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	response := MetroConnectivityResponse{
		Metros:       []MetroInfo{},
		Connectivity: []MetroConnectivity{},
	}

	// Helper to run Neo4j query with retry
	runNeo4jQuery := func(cypher string) ([]*neo4jdriver.Record, error) {
		cfg := dberror.DefaultRetryConfig()
		return dberror.Retry(ctx, cfg, func() ([]*neo4jdriver.Record, error) {
			session := config.Neo4jSession(ctx)
			defer session.Close(ctx)

			result, err := session.Run(ctx, cypher, nil)
			if err != nil {
				return nil, err
			}
			return result.Collect(ctx)
		})
	}

	// First, get all metros that have ISIS-enabled devices
	metroCypher := `
		MATCH (m:Metro)<-[:LOCATED_IN]-(d:Device)
		WHERE d.isis_system_id IS NOT NULL
		WITH m, count(d) AS deviceCount
		WHERE deviceCount > 0
		RETURN m.pk AS pk, m.code AS code, m.name AS name
		ORDER BY m.code
	`

	metroRecords, err := runNeo4jQuery(metroCypher)
	if err != nil {
		log.Printf("Metro connectivity metro query error: %v", err)
		response.Error = dberror.UserMessage(err)
		writeJSON(w, response)
		return
	}

	metroMap := make(map[string]MetroInfo)
	for _, record := range metroRecords {
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		name, _ := record.Get("name")

		metro := MetroInfo{
			PK:   asString(pk),
			Code: asString(code),
			Name: asString(name),
		}
		response.Metros = append(response.Metros, metro)
		metroMap[metro.PK] = metro
	}

	// For each pair of metros, find the best path between any devices in those metros
	// This query finds the shortest path between any two ISIS devices in different metros
	// and calculates bottleneck bandwidth (min bandwidth along path)
	connectivityCypher := `
		MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
		MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
		WHERE m1.pk < m2.pk
		  AND d1.isis_system_id IS NOT NULL
		  AND d2.isis_system_id IS NOT NULL
		WITH m1, m2, d1, d2
		MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
		WITH m1, m2,
		     length(path) AS hops,
		     reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS metric,
		     reduce(minBw = 9999999999999, r IN relationships(path) |
		       CASE WHEN coalesce(r.bandwidth_bps, 9999999999999) < minBw
		            THEN coalesce(r.bandwidth_bps, 9999999999999) ELSE minBw END) AS bottleneckBw
		WITH m1, m2, min(hops) AS minHops, min(metric) AS minMetric, count(*) AS pathCount,
		     max(bottleneckBw) AS maxBottleneckBw
		RETURN m1.pk AS fromPK, m1.code AS fromCode, m1.name AS fromName,
		       m2.pk AS toPK, m2.code AS toCode, m2.name AS toName,
		       minHops, minMetric, pathCount, maxBottleneckBw
		ORDER BY fromCode, toCode
	`

	connRecords, err := runNeo4jQuery(connectivityCypher)
	if err != nil {
		log.Printf("Metro connectivity query error: %v", err)
		response.Error = dberror.UserMessage(err)
		writeJSON(w, response)
		return
	}

	for _, record := range connRecords {
		fromPK, _ := record.Get("fromPK")
		fromCode, _ := record.Get("fromCode")
		fromName, _ := record.Get("fromName")
		toPK, _ := record.Get("toPK")
		toCode, _ := record.Get("toCode")
		toName, _ := record.Get("toName")
		minHops, _ := record.Get("minHops")
		minMetric, _ := record.Get("minMetric")
		pathCount, _ := record.Get("pathCount")
		maxBottleneckBw, _ := record.Get("maxBottleneckBw")

		// Convert bandwidth from bps to Gbps, handle sentinel value
		bottleneckBwGbps := 0.0
		bwBps := asFloat64(maxBottleneckBw)
		if bwBps > 0 && bwBps < 9999999999999 {
			bottleneckBwGbps = bwBps / 1e9
		}

		// Add both directions (matrix is symmetric)
		conn := MetroConnectivity{
			FromMetroPK:      asString(fromPK),
			FromMetroCode:    asString(fromCode),
			FromMetroName:    asString(fromName),
			ToMetroPK:        asString(toPK),
			ToMetroCode:      asString(toCode),
			ToMetroName:      asString(toName),
			PathCount:        int(asInt64(pathCount)),
			MinHops:          int(asInt64(minHops)),
			MinMetric:        asInt64(minMetric),
			BottleneckBwGbps: bottleneckBwGbps,
		}
		response.Connectivity = append(response.Connectivity, conn)

		// Add reverse direction
		connReverse := MetroConnectivity{
			FromMetroPK:      asString(toPK),
			FromMetroCode:    asString(toCode),
			FromMetroName:    asString(toName),
			ToMetroPK:        asString(fromPK),
			ToMetroCode:      asString(fromCode),
			ToMetroName:      asString(fromName),
			PathCount:        int(asInt64(pathCount)),
			MinHops:          int(asInt64(minHops)),
			MinMetric:        asInt64(minMetric),
			BottleneckBwGbps: bottleneckBwGbps,
		}
		response.Connectivity = append(response.Connectivity, connReverse)
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil) // Reuse existing metric for now

	log.Printf("Metro connectivity returned %d metros, %d connections in %v",
		len(response.Metros), len(response.Connectivity), duration)

	writeJSON(w, response)
}

// MetroPathLatency represents path-based latency between two metros
type MetroPathLatency struct {
	FromMetroPK       string  `json:"fromMetroPK"`
	FromMetroCode     string  `json:"fromMetroCode"`
	ToMetroPK         string  `json:"toMetroPK"`
	ToMetroCode       string  `json:"toMetroCode"`
	PathLatencyMs     float64 `json:"pathLatencyMs"`     // Sum of link metrics along path (in ms)
	HopCount          int     `json:"hopCount"`          // Number of hops
	BottleneckBwGbps  float64 `json:"bottleneckBwGbps"`  // Min bandwidth along path (Gbps)
	InternetLatencyMs float64 `json:"internetLatencyMs"` // Internet latency for comparison (0 if not available)
	ImprovementPct    float64 `json:"improvementPct"`    // Improvement vs internet (0 if no internet data)
}

// MetroPathLatencyResponse is the response for the metro path latency endpoint
type MetroPathLatencyResponse struct {
	Optimize string             `json:"optimize"` // "hops", "latency", or "bandwidth"
	Paths    []MetroPathLatency `json:"paths"`
	Summary  struct {
		TotalPairs        int     `json:"totalPairs"`
		PairsWithInternet int     `json:"pairsWithInternet"`
		AvgImprovementPct float64 `json:"avgImprovementPct"`
		MaxImprovementPct float64 `json:"maxImprovementPct"`
	} `json:"summary"`
	Error string `json:"error,omitempty"`
}

// GetMetroPathLatency returns path-based latency between all metro pairs
// with configurable optimization strategy (hops, latency, or bandwidth)
func GetMetroPathLatency(w http.ResponseWriter, r *http.Request) {
	optimize := r.URL.Query().Get("optimize")
	if optimize == "" {
		optimize = "latency" // default to latency optimization
	}
	if optimize != "hops" && optimize != "latency" && optimize != "bandwidth" {
		writeJSON(w, MetroPathLatencyResponse{Error: "optimize must be 'hops', 'latency', or 'bandwidth'"})
		return
	}

	// Try cache first
	if statusCache != nil {
		if cached := statusCache.GetMetroPathLatency(optimize); cached != nil {
			w.Header().Set("X-Cache", "HIT")
			writeJSON(w, cached)
			return
		}
	}

	// Cache miss - fetch fresh data
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MetroPathLatencyResponse{
		Optimize: optimize,
		Paths:    []MetroPathLatency{},
	}

	// Build Cypher query based on optimization strategy
	var cypher string
	if optimize == "latency" {
		// Use Dijkstra for latency-optimized paths
		cypher = `
			MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
			WHERE m1.pk < m2.pk
			  AND d1.isis_system_id IS NOT NULL
			  AND d2.isis_system_id IS NOT NULL
			WITH m1, m2, d1, d2
			CALL apoc.algo.dijkstra(d1, d2, 'ISIS_ADJACENT', 'metric') YIELD path, weight
			WITH m1, m2, path, weight,
			     length(path) AS hops,
			     reduce(minBw = 9999999999999, r IN relationships(path) |
			       CASE WHEN coalesce(r.bandwidth_bps, 9999999999999) < minBw
			            THEN coalesce(r.bandwidth_bps, 9999999999999) ELSE minBw END) AS bottleneckBw
			WITH m1, m2,
			     min(weight) AS bestMetric,
			     min(hops) AS bestHops,
			     max(bottleneckBw) AS bestBottleneck
			RETURN m1.pk AS fromPK, m1.code AS fromCode,
			       m2.pk AS toPK, m2.code AS toCode,
			       bestMetric AS metric, bestHops AS hops, bestBottleneck AS bottleneck
			ORDER BY fromCode, toCode
		`
	} else if optimize == "bandwidth" {
		// For bandwidth, we want the path with maximum bottleneck bandwidth
		// This requires finding all paths and selecting the widest one
		cypher = `
			MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
			WHERE m1.pk < m2.pk
			  AND d1.isis_system_id IS NOT NULL
			  AND d2.isis_system_id IS NOT NULL
			WITH m1, m2, d1, d2
			MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
			WITH m1, m2, path,
			     length(path) AS hops,
			     reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS metric,
			     reduce(minBw = 9999999999999, r IN relationships(path) |
			       CASE WHEN coalesce(r.bandwidth_bps, 9999999999999) < minBw
			            THEN coalesce(r.bandwidth_bps, 9999999999999) ELSE minBw END) AS bottleneckBw
			WITH m1, m2, hops, metric, bottleneckBw
			ORDER BY m1.pk, m2.pk, bottleneckBw DESC
			WITH m1, m2, collect({hops: hops, metric: metric, bottleneck: bottleneckBw})[0] AS best
			RETURN m1.pk AS fromPK, m1.code AS fromCode,
			       m2.pk AS toPK, m2.code AS toCode,
			       best.metric AS metric, best.hops AS hops, best.bottleneck AS bottleneck
			ORDER BY fromCode, toCode
		`
	} else {
		// Default: fewest hops
		cypher = `
			MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
			WHERE m1.pk < m2.pk
			  AND d1.isis_system_id IS NOT NULL
			  AND d2.isis_system_id IS NOT NULL
			WITH m1, m2, d1, d2
			MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
			WITH m1, m2, path,
			     length(path) AS hops,
			     reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS metric,
			     reduce(minBw = 9999999999999, r IN relationships(path) |
			       CASE WHEN coalesce(r.bandwidth_bps, 9999999999999) < minBw
			            THEN coalesce(r.bandwidth_bps, 9999999999999) ELSE minBw END) AS bottleneckBw
			WITH m1, m2, min(hops) AS bestHops, min(metric) AS bestMetric, max(bottleneckBw) AS bestBottleneck
			RETURN m1.pk AS fromPK, m1.code AS fromCode,
			       m2.pk AS toPK, m2.code AS toCode,
			       bestMetric AS metric, bestHops AS hops, bestBottleneck AS bottleneck
			ORDER BY fromCode, toCode
		`
	}

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		log.Printf("Metro path latency query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	records, err := result.Collect(ctx)
	if err != nil {
		log.Printf("Metro path latency collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	// Build map of metro paths
	pathMap := make(map[string]*MetroPathLatency)
	for _, record := range records {
		fromPK, _ := record.Get("fromPK")
		fromCode, _ := record.Get("fromCode")
		toPK, _ := record.Get("toPK")
		toCode, _ := record.Get("toCode")
		metric, _ := record.Get("metric")
		hops, _ := record.Get("hops")
		bottleneck, _ := record.Get("bottleneck")

		metricVal := asFloat64(metric)
		bottleneckVal := asFloat64(bottleneck)
		if bottleneckVal > 1e12 {
			bottleneckVal = 0 // No bandwidth data
		}

		path := &MetroPathLatency{
			FromMetroPK:      asString(fromPK),
			FromMetroCode:    asString(fromCode),
			ToMetroPK:        asString(toPK),
			ToMetroCode:      asString(toCode),
			PathLatencyMs:    metricVal / 1000.0, // Convert microseconds to milliseconds
			HopCount:         int(asInt64(hops)),
			BottleneckBwGbps: bottleneckVal / 1e9, // Convert bps to Gbps
		}

		// Store in map for both directions
		key1 := asString(fromCode) + ":" + asString(toCode)
		key2 := asString(toCode) + ":" + asString(fromCode)
		pathMap[key1] = path
		pathMap[key2] = &MetroPathLatency{
			FromMetroPK:      asString(toPK),
			FromMetroCode:    asString(toCode),
			ToMetroPK:        asString(fromPK),
			ToMetroCode:      asString(fromCode),
			PathLatencyMs:    path.PathLatencyMs,
			HopCount:         path.HopCount,
			BottleneckBwGbps: path.BottleneckBwGbps,
		}
	}

	// Now fetch internet latency data from ClickHouse for comparison
	internetQuery := `
		SELECT
			least(ma.code, mz.code) AS metro1,
			greatest(ma.code, mz.code) AS metro2,
			round(avg(f.rtt_us) / 1000.0, 2) AS avg_rtt_ms
		FROM fact_dz_internet_metro_latency f
		JOIN dz_metros_current ma ON f.origin_metro_pk = ma.pk
		JOIN dz_metros_current mz ON f.target_metro_pk = mz.pk
		WHERE f.event_ts >= now() - INTERVAL 24 HOUR
		  AND ma.code != mz.code
		GROUP BY metro1, metro2
	`

	rows, err := config.DB.Query(ctx, internetQuery)
	if err != nil {
		log.Printf("Metro path latency internet query error: %v", err)
		// Continue without internet data
	} else {
		defer rows.Close()
		for rows.Next() {
			var metro1, metro2 string
			var avgRttMs float64
			if err := rows.Scan(&metro1, &metro2, &avgRttMs); err != nil {
				response.Error = fmt.Sprintf("failed to scan internet latency row: %v", err)
				writeJSON(w, response)
				return
			}
			// Update both directions in pathMap
			key1 := metro1 + ":" + metro2
			key2 := metro2 + ":" + metro1
			if p, ok := pathMap[key1]; ok {
				p.InternetLatencyMs = avgRttMs
				if avgRttMs > 0 && p.PathLatencyMs > 0 {
					p.ImprovementPct = (avgRttMs - p.PathLatencyMs) / avgRttMs * 100
				}
			}
			if p, ok := pathMap[key2]; ok {
				p.InternetLatencyMs = avgRttMs
				if avgRttMs > 0 && p.PathLatencyMs > 0 {
					p.ImprovementPct = (avgRttMs - p.PathLatencyMs) / avgRttMs * 100
				}
			}
		}
	}

	// Convert map to slice and compute summary
	var totalImprovement float64
	var maxImprovement float64
	var pairsWithInternet int

	for _, path := range pathMap {
		response.Paths = append(response.Paths, *path)
		if path.InternetLatencyMs > 0 {
			pairsWithInternet++
			totalImprovement += path.ImprovementPct
			if path.ImprovementPct > maxImprovement {
				maxImprovement = path.ImprovementPct
			}
		}
	}

	response.Summary.TotalPairs = len(response.Paths)
	response.Summary.PairsWithInternet = pairsWithInternet
	if pairsWithInternet > 0 {
		response.Summary.AvgImprovementPct = totalImprovement / float64(pairsWithInternet)
	}
	response.Summary.MaxImprovementPct = maxImprovement

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Metro path latency (%s) returned %d paths in %v",
		optimize, len(response.Paths), duration)

	writeJSON(w, response)
}

// fetchMetroPathLatencyData fetches metro path latency data for the given optimization strategy.
// Used by both the handler and the cache.
func fetchMetroPathLatencyData(ctx context.Context, optimize string) (*MetroPathLatencyResponse, error) {
	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := &MetroPathLatencyResponse{
		Optimize: optimize,
		Paths:    []MetroPathLatency{},
	}

	// Build Cypher query based on optimization strategy
	var cypher string
	if optimize == "latency" {
		cypher = `
			MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
			WHERE m1.pk < m2.pk
			  AND d1.isis_system_id IS NOT NULL
			  AND d2.isis_system_id IS NOT NULL
			WITH m1, m2, d1, d2
			CALL apoc.algo.dijkstra(d1, d2, 'ISIS_ADJACENT', 'metric') YIELD path, weight
			WITH m1, m2, path, weight,
			     length(path) AS hops,
			     reduce(minBw = 9999999999999, r IN relationships(path) |
			       CASE WHEN coalesce(r.bandwidth_bps, 9999999999999) < minBw
			            THEN coalesce(r.bandwidth_bps, 9999999999999) ELSE minBw END) AS bottleneckBw
			WITH m1, m2,
			     min(weight) AS bestMetric,
			     min(hops) AS bestHops,
			     max(bottleneckBw) AS bestBottleneck
			RETURN m1.pk AS fromPK, m1.code AS fromCode,
			       m2.pk AS toPK, m2.code AS toCode,
			       bestMetric AS metric, bestHops AS hops, bestBottleneck AS bottleneck
			ORDER BY fromCode, toCode
		`
	} else if optimize == "bandwidth" {
		cypher = `
			MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
			WHERE m1.pk < m2.pk
			  AND d1.isis_system_id IS NOT NULL
			  AND d2.isis_system_id IS NOT NULL
			WITH m1, m2, d1, d2
			MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
			WITH m1, m2, path,
			     length(path) AS hops,
			     reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS metric,
			     reduce(minBw = 9999999999999, r IN relationships(path) |
			       CASE WHEN coalesce(r.bandwidth_bps, 9999999999999) < minBw
			            THEN coalesce(r.bandwidth_bps, 9999999999999) ELSE minBw END) AS bottleneckBw
			WITH m1, m2, hops, metric, bottleneckBw
			ORDER BY m1.pk, m2.pk, bottleneckBw DESC
			WITH m1, m2, collect({hops: hops, metric: metric, bottleneck: bottleneckBw})[0] AS best
			RETURN m1.pk AS fromPK, m1.code AS fromCode,
			       m2.pk AS toPK, m2.code AS toCode,
			       best.metric AS metric, best.hops AS hops, best.bottleneck AS bottleneck
			ORDER BY fromCode, toCode
		`
	} else {
		// Default: fewest hops
		cypher = `
			MATCH (m1:Metro)<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro)<-[:LOCATED_IN]-(d2:Device)
			WHERE m1.pk < m2.pk
			  AND d1.isis_system_id IS NOT NULL
			  AND d2.isis_system_id IS NOT NULL
			WITH m1, m2, d1, d2
			MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
			WITH m1, m2, path,
			     length(path) AS hops,
			     reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS metric,
			     reduce(minBw = 9999999999999, r IN relationships(path) |
			       CASE WHEN coalesce(r.bandwidth_bps, 9999999999999) < minBw
			            THEN coalesce(r.bandwidth_bps, 9999999999999) ELSE minBw END) AS bottleneckBw
			WITH m1, m2, min(hops) AS bestHops, min(metric) AS bestMetric, max(bottleneckBw) AS bestBottleneck
			RETURN m1.pk AS fromPK, m1.code AS fromCode,
			       m2.pk AS toPK, m2.code AS toCode,
			       bestMetric AS metric, bestHops AS hops, bestBottleneck AS bottleneck
			ORDER BY fromCode, toCode
		`
	}

	result, err := session.Run(ctx, cypher, nil)
	if err != nil {
		return nil, fmt.Errorf("neo4j query error: %w", err)
	}

	records, err := result.Collect(ctx)
	if err != nil {
		return nil, fmt.Errorf("neo4j collect error: %w", err)
	}

	// Build map of metro paths
	pathMap := make(map[string]*MetroPathLatency)
	for _, record := range records {
		fromPK, _ := record.Get("fromPK")
		fromCode, _ := record.Get("fromCode")
		toPK, _ := record.Get("toPK")
		toCode, _ := record.Get("toCode")
		metric, _ := record.Get("metric")
		hops, _ := record.Get("hops")
		bottleneck, _ := record.Get("bottleneck")

		metricVal := asFloat64(metric)
		bottleneckVal := asFloat64(bottleneck)
		if bottleneckVal > 1e12 {
			bottleneckVal = 0 // No bandwidth data
		}

		path := &MetroPathLatency{
			FromMetroPK:      asString(fromPK),
			FromMetroCode:    asString(fromCode),
			ToMetroPK:        asString(toPK),
			ToMetroCode:      asString(toCode),
			PathLatencyMs:    metricVal / 1000.0, // Convert microseconds to milliseconds
			HopCount:         int(asInt64(hops)),
			BottleneckBwGbps: bottleneckVal / 1e9, // Convert bps to Gbps
		}

		// Store in map for both directions
		key1 := asString(fromCode) + ":" + asString(toCode)
		key2 := asString(toCode) + ":" + asString(fromCode)
		pathMap[key1] = path
		pathMap[key2] = &MetroPathLatency{
			FromMetroPK:      asString(toPK),
			FromMetroCode:    asString(toCode),
			ToMetroPK:        asString(fromPK),
			ToMetroCode:      asString(fromCode),
			PathLatencyMs:    path.PathLatencyMs,
			HopCount:         path.HopCount,
			BottleneckBwGbps: path.BottleneckBwGbps,
		}
	}

	// Fetch internet latency data from ClickHouse for comparison
	internetQuery := `
		SELECT
			least(ma.code, mz.code) AS metro1,
			greatest(ma.code, mz.code) AS metro2,
			round(avg(f.rtt_us) / 1000.0, 2) AS avg_rtt_ms
		FROM fact_dz_internet_metro_latency f
		JOIN dz_metros_current ma ON f.origin_metro_pk = ma.pk
		JOIN dz_metros_current mz ON f.target_metro_pk = mz.pk
		WHERE f.event_ts >= now() - INTERVAL 24 HOUR
		  AND ma.code != mz.code
		GROUP BY metro1, metro2
	`

	rows, err := config.DB.Query(ctx, internetQuery)
	if err != nil {
		log.Printf("Metro path latency internet query error: %v", err)
		// Continue without internet data
	} else {
		defer rows.Close()
		for rows.Next() {
			var metro1, metro2 string
			var avgRttMs float64
			if err := rows.Scan(&metro1, &metro2, &avgRttMs); err != nil {
				return nil, fmt.Errorf("failed to scan internet latency row: %w", err)
			}
			// Update both directions in pathMap
			key1 := metro1 + ":" + metro2
			key2 := metro2 + ":" + metro1
			if p, ok := pathMap[key1]; ok {
				p.InternetLatencyMs = avgRttMs
				if avgRttMs > 0 && p.PathLatencyMs > 0 {
					p.ImprovementPct = (avgRttMs - p.PathLatencyMs) / avgRttMs * 100
				}
			}
			if p, ok := pathMap[key2]; ok {
				p.InternetLatencyMs = avgRttMs
				if avgRttMs > 0 && p.PathLatencyMs > 0 {
					p.ImprovementPct = (avgRttMs - p.PathLatencyMs) / avgRttMs * 100
				}
			}
		}
	}

	// Convert map to slice and compute summary
	var totalImprovement float64
	var maxImprovement float64
	var pairsWithInternet int

	for _, path := range pathMap {
		response.Paths = append(response.Paths, *path)
		if path.InternetLatencyMs > 0 {
			pairsWithInternet++
			totalImprovement += path.ImprovementPct
			if path.ImprovementPct > maxImprovement {
				maxImprovement = path.ImprovementPct
			}
		}
	}

	response.Summary.TotalPairs = len(response.Paths)
	response.Summary.PairsWithInternet = pairsWithInternet
	if pairsWithInternet > 0 {
		response.Summary.AvgImprovementPct = totalImprovement / float64(pairsWithInternet)
	}
	response.Summary.MaxImprovementPct = maxImprovement

	duration := time.Since(start)
	log.Printf("fetchMetroPathLatencyData (%s) returned %d paths in %v",
		optimize, len(response.Paths), duration)

	return response, nil
}

// MetroPathDetailHop represents a single hop in a path
type MetroPathDetailHop struct {
	DevicePK    string  `json:"devicePK"`
	DeviceCode  string  `json:"deviceCode"`
	MetroPK     string  `json:"metroPK"`
	MetroCode   string  `json:"metroCode"`
	LinkMetric  int64   `json:"linkMetric"`  // Metric to next hop (0 for last hop)
	LinkBwGbps  float64 `json:"linkBwGbps"`  // Bandwidth to next hop (0 for last hop)
	LinkLatency float64 `json:"linkLatency"` // Latency in ms to next hop
}

// MetroPathDetailResponse is the response for the metro path detail endpoint
type MetroPathDetailResponse struct {
	FromMetroCode     string               `json:"fromMetroCode"`
	ToMetroCode       string               `json:"toMetroCode"`
	Optimize          string               `json:"optimize"`
	TotalLatencyMs    float64              `json:"totalLatencyMs"`
	TotalHops         int                  `json:"totalHops"`
	BottleneckBwGbps  float64              `json:"bottleneckBwGbps"`
	InternetLatencyMs float64              `json:"internetLatencyMs"`
	ImprovementPct    float64              `json:"improvementPct"`
	Hops              []MetroPathDetailHop `json:"hops"`
	Error             string               `json:"error,omitempty"`
}

// GetMetroPathDetail returns detailed path breakdown between two metros
func GetMetroPathDetail(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	fromCode := r.URL.Query().Get("from")
	toCode := r.URL.Query().Get("to")
	optimize := r.URL.Query().Get("optimize")

	if fromCode == "" || toCode == "" {
		writeJSON(w, MetroPathDetailResponse{Error: "from and to parameters are required"})
		return
	}
	if optimize == "" {
		optimize = "latency"
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MetroPathDetailResponse{
		FromMetroCode: fromCode,
		ToMetroCode:   toCode,
		Optimize:      optimize,
		Hops:          []MetroPathDetailHop{},
	}

	// Build query based on optimization mode
	var cypher string
	if optimize == "latency" {
		cypher = `
			MATCH (m1:Metro {code: $from})<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro {code: $to})<-[:LOCATED_IN]-(d2:Device)
			WHERE d1.isis_system_id IS NOT NULL AND d2.isis_system_id IS NOT NULL
			WITH d1, d2
			CALL apoc.algo.dijkstra(d1, d2, 'ISIS_ADJACENT', 'metric') YIELD path, weight
			WITH path, weight
			ORDER BY weight
			LIMIT 1
			WITH path, nodes(path) AS pathNodes, relationships(path) AS pathRels
			UNWIND range(0, size(pathNodes)-1) AS idx
			WITH pathNodes, pathRels, pathNodes[idx] AS node,
			     CASE WHEN idx < size(pathRels) THEN pathRels[idx] ELSE null END AS rel
			MATCH (node)-[:LOCATED_IN]->(m:Metro)
			RETURN node.pk AS devicePK, node.code AS deviceCode,
			       m.pk AS metroPK, m.code AS metroCode,
			       coalesce(rel.metric, 0) AS linkMetric,
			       coalesce(rel.bandwidth_bps, 0) AS linkBw
		`
	} else {
		cypher = `
			MATCH (m1:Metro {code: $from})<-[:LOCATED_IN]-(d1:Device)
			MATCH (m2:Metro {code: $to})<-[:LOCATED_IN]-(d2:Device)
			WHERE d1.isis_system_id IS NOT NULL AND d2.isis_system_id IS NOT NULL
			WITH d1, d2
			MATCH path = shortestPath((d1)-[:ISIS_ADJACENT*]-(d2))
			WITH path
			ORDER BY length(path)
			LIMIT 1
			WITH path, nodes(path) AS pathNodes, relationships(path) AS pathRels
			UNWIND range(0, size(pathNodes)-1) AS idx
			WITH pathNodes, pathRels, pathNodes[idx] AS node,
			     CASE WHEN idx < size(pathRels) THEN pathRels[idx] ELSE null END AS rel
			MATCH (node)-[:LOCATED_IN]->(m:Metro)
			RETURN node.pk AS devicePK, node.code AS deviceCode,
			       m.pk AS metroPK, m.code AS metroCode,
			       coalesce(rel.metric, 0) AS linkMetric,
			       coalesce(rel.bandwidth_bps, 0) AS linkBw
		`
	}

	result, err := session.Run(ctx, cypher, map[string]any{
		"from": fromCode,
		"to":   toCode,
	})
	if err != nil {
		log.Printf("Metro path detail query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	records, err := result.Collect(ctx)
	if err != nil {
		log.Printf("Metro path detail collect error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	if len(records) == 0 {
		response.Error = "No path found between metros"
		writeJSON(w, response)
		return
	}

	var totalMetric int64
	var minBandwidth float64 = 1e15

	for _, record := range records {
		devicePK, _ := record.Get("devicePK")
		deviceCode, _ := record.Get("deviceCode")
		metroPK, _ := record.Get("metroPK")
		metroCode, _ := record.Get("metroCode")
		linkMetric, _ := record.Get("linkMetric")
		linkBw, _ := record.Get("linkBw")

		metric := asInt64(linkMetric)
		bw := asFloat64(linkBw)

		hop := MetroPathDetailHop{
			DevicePK:    asString(devicePK),
			DeviceCode:  asString(deviceCode),
			MetroPK:     asString(metroPK),
			MetroCode:   asString(metroCode),
			LinkMetric:  metric,
			LinkLatency: float64(metric) / 1000.0, // Convert to ms
			LinkBwGbps:  bw / 1e9,
		}

		response.Hops = append(response.Hops, hop)
		totalMetric += metric
		if bw > 0 && bw < minBandwidth {
			minBandwidth = bw
		}
	}

	response.TotalLatencyMs = float64(totalMetric) / 1000.0
	response.TotalHops = len(response.Hops) - 1
	if minBandwidth < 1e15 {
		response.BottleneckBwGbps = minBandwidth / 1e9
	}

	// Fetch internet latency for comparison
	internetQuery := `
		SELECT round(avg(f.rtt_us) / 1000.0, 2) AS avg_rtt_ms
		FROM fact_dz_internet_metro_latency f
		JOIN dz_metros_current ma ON f.origin_metro_pk = ma.pk
		JOIN dz_metros_current mz ON f.target_metro_pk = mz.pk
		WHERE f.event_ts >= now() - INTERVAL 24 HOUR
		  AND ((ma.code = $1 AND mz.code = $2) OR (ma.code = $2 AND mz.code = $1))
	`

	var internetLatency float64
	row := config.DB.QueryRow(ctx, internetQuery, fromCode, toCode)
	if err := row.Scan(&internetLatency); err == nil && internetLatency > 0 {
		response.InternetLatencyMs = internetLatency
		if response.TotalLatencyMs > 0 {
			response.ImprovementPct = (internetLatency - response.TotalLatencyMs) / internetLatency * 100
		}
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	writeJSON(w, response)
}

// MetroPathsHop represents a device in a path
type MetroPathsHop struct {
	DevicePK   string `json:"devicePK"`
	DeviceCode string `json:"deviceCode"`
	MetroPK    string `json:"metroPK"`
	MetroCode  string `json:"metroCode"`
}

// MetroPath represents a single path between metros
type MetroPath struct {
	Hops        []MetroPathsHop `json:"hops"`
	TotalHops   int             `json:"totalHops"`
	TotalMetric int64           `json:"totalMetric"`
	LatencyMs   float64         `json:"latencyMs"`
}

// MetroPathsResponse is the response for metro paths endpoint
type MetroPathsResponse struct {
	FromMetroCode string      `json:"fromMetroCode"`
	ToMetroCode   string      `json:"toMetroCode"`
	Paths         []MetroPath `json:"paths"`
	Error         string      `json:"error,omitempty"`
}

// GetMetroPaths returns distinct paths between two metros
func GetMetroPaths(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	fromPK := r.URL.Query().Get("from")
	toPK := r.URL.Query().Get("to")
	kStr := r.URL.Query().Get("k")
	if kStr == "" {
		kStr = "5"
	}
	k, _ := strconv.Atoi(kStr)
	if k <= 0 || k > 10 {
		k = 5
	}

	if fromPK == "" || toPK == "" {
		http.Error(w, "from and to metro PKs are required", http.StatusBadRequest)
		return
	}

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MetroPathsResponse{
		Paths: []MetroPath{},
	}

	// Get metro codes first
	metroCypher := `
		MATCH (m1:Metro {pk: $fromPK}), (m2:Metro {pk: $toPK})
		RETURN m1.code AS fromCode, m2.code AS toCode
	`
	metroResult, err := session.Run(ctx, metroCypher, map[string]interface{}{
		"fromPK": fromPK,
		"toPK":   toPK,
	})
	if err != nil {
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}
	if metroResult.Next(ctx) {
		record := metroResult.Record()
		fromCode, _ := record.Get("fromCode")
		toCode, _ := record.Get("toCode")
		response.FromMetroCode = asString(fromCode)
		response.ToMetroCode = asString(toCode)
	}

	// Find k-shortest paths between any devices in the two metros using Yen's algorithm
	pathsCypher := `
		MATCH (m1:Metro {pk: $fromPK})<-[:LOCATED_IN]-(d1:Device)
		MATCH (m2:Metro {pk: $toPK})<-[:LOCATED_IN]-(d2:Device)
		WHERE d1.isis_system_id IS NOT NULL AND d2.isis_system_id IS NOT NULL
		WITH d1, d2
		CALL apoc.algo.dijkstra(d1, d2, 'ISIS_ADJACENT', 'metric') YIELD path, weight
		WITH path, weight
		ORDER BY weight
		LIMIT $k
		WITH path, weight, nodes(path) AS pathNodes
		UNWIND range(0, size(pathNodes)-1) AS idx
		WITH path, weight, pathNodes, idx, pathNodes[idx] AS node
		OPTIONAL MATCH (node)-[:LOCATED_IN]->(m:Metro)
		WITH path, weight, idx, node, m
		ORDER BY path, idx
		WITH path, weight, collect({pk: node.pk, code: node.code, metroPK: m.pk, metroCode: m.code}) AS hopList
		RETURN hopList, weight AS totalMetric, size(hopList)-1 AS totalHops
	`

	result, err := session.Run(ctx, pathsCypher, map[string]interface{}{
		"fromPK": fromPK,
		"toPK":   toPK,
		"k":      k,
	})
	if err != nil {
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	records, err := result.Collect(ctx)
	if err != nil {
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	for _, record := range records {
		hopListVal, _ := record.Get("hopList")
		totalMetricVal, _ := record.Get("totalMetric")
		totalHopsVal, _ := record.Get("totalHops")

		hopList, _ := hopListVal.([]interface{})
		totalMetric := asInt64(totalMetricVal)
		totalHops := int(asInt64(totalHopsVal))

		path := MetroPath{
			Hops:        []MetroPathsHop{},
			TotalHops:   totalHops,
			TotalMetric: totalMetric,
			LatencyMs:   float64(totalMetric) / 1000.0, // Convert microseconds to ms
		}

		for _, hopVal := range hopList {
			hopMap, ok := hopVal.(map[string]interface{})
			if !ok {
				continue
			}

			hop := MetroPathsHop{
				DevicePK:   asString(hopMap["pk"]),
				DeviceCode: asString(hopMap["code"]),
				MetroPK:    asString(hopMap["metroPK"]),
				MetroCode:  asString(hopMap["metroCode"]),
			}

			path.Hops = append(path.Hops, hop)
		}

		response.Paths = append(response.Paths, path)
	}

	writeJSON(w, response)
}

// MaintenanceImpactRequest is the request body for maintenance impact analysis
type MaintenanceImpactRequest struct {
	Devices []string `json:"devices"` // Device PKs to take offline
	Links   []string `json:"links"`   // Link PKs to take offline (as "sourcePK:targetPK")
}

// MaintenanceItem represents a device or link being taken offline
type MaintenanceItem struct {
	Type            string   `json:"type"`            // "device" or "link"
	PK              string   `json:"pk"`              // Device PK or link PK
	Code            string   `json:"code"`            // Device code or "sourceCode - targetCode"
	Impact          int      `json:"impact"`          // Number of affected paths/devices
	Disconnected    int      `json:"disconnected"`    // Devices that would lose connectivity
	CausesPartition bool     `json:"causesPartition"` // Would this cause a network partition?
	DisconnectedDevices []string `json:"disconnectedDevices,omitempty"` // Device codes that would be disconnected
}

// MaintenanceAffectedPath represents a path that would be impacted by maintenance
type MaintenanceAffectedPath struct {
	Source       string `json:"source"`       // Source device code
	Target       string `json:"target"`       // Target device code
	SourceMetro  string `json:"sourceMetro"`  // Source metro code
	TargetMetro  string `json:"targetMetro"`  // Target metro code
	HopsBefore   int    `json:"hopsBefore"`   // Hops before maintenance
	HopsAfter    int    `json:"hopsAfter"`    // Hops after maintenance (-1 = disconnected)
	MetricBefore int    `json:"metricBefore"` // Total ISIS metric before
	MetricAfter  int    `json:"metricAfter"`  // Total ISIS metric after (-1 = disconnected)
	Status       string `json:"status"`       // "rerouted", "degraded", or "disconnected"
}

// AffectedLink represents a specific link affected by maintenance
type AffectedLink struct {
	SourceDevice string `json:"sourceDevice"` // Device code in source metro
	TargetDevice string `json:"targetDevice"` // Device code in target metro
	Status       string `json:"status"`       // "offline" (device going down) or "rerouted"
}

// AffectedMetroPair represents connectivity impact between two metros
type AffectedMetroPair struct {
	SourceMetro   string         `json:"sourceMetro"`
	TargetMetro   string         `json:"targetMetro"`
	AffectedLinks []AffectedLink `json:"affectedLinks"` // Specific links affected
	Status        string         `json:"status"`        // "reduced", "degraded", or "disconnected"
}

// MaintenanceImpactResponse is the response for maintenance impact analysis
type MaintenanceImpactResponse struct {
	Items             []MaintenanceItem           `json:"items"`                       // Items with their individual impacts
	TotalImpact       int                         `json:"totalImpact"`                 // Total affected paths when all items are down
	TotalDisconnected int                         `json:"totalDisconnected"`           // Total devices that lose connectivity
	RecommendedOrder  []string                    `json:"recommendedOrder"`            // PKs in recommended maintenance order (least impact first)
	AffectedPaths     []MaintenanceAffectedPath   `json:"affectedPaths,omitempty"`     // Sample of affected paths
	AffectedMetros    []AffectedMetroPair         `json:"affectedMetros,omitempty"`    // Affected metro pairs
	DisconnectedList  []string                    `json:"disconnectedList,omitempty"`  // All devices that would be disconnected
	Error             string                      `json:"error,omitempty"`
}

// PostMaintenanceImpact analyzes the impact of taking multiple devices/links offline
func PostMaintenanceImpact(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	// Parse request body
	var req MaintenanceImpactRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, MaintenanceImpactResponse{Error: "Invalid request body: " + err.Error()})
		return
	}

	if len(req.Devices) == 0 && len(req.Links) == 0 {
		writeJSON(w, MaintenanceImpactResponse{Error: "No devices or links specified"})
		return
	}

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MaintenanceImpactResponse{
		Items:            []MaintenanceItem{},
		RecommendedOrder: []string{},
		AffectedPaths:    []MaintenanceAffectedPath{},
		AffectedMetros:   []AffectedMetroPair{},
		DisconnectedList: []string{},
	}

	// Collect all device PKs and link endpoints being taken offline
	offlineDevicePKs := make(map[string]bool)
	offlineLinkEndpoints := make(map[string]bool) // "sourcePK:targetPK" format

	for _, pk := range req.Devices {
		offlineDevicePKs[pk] = true
	}

	// Batch analyze all devices in a single query
	if len(req.Devices) > 0 {
		deviceItems := analyzeDevicesImpactBatch(ctx, session, req.Devices)
		for _, item := range deviceItems {
			response.Items = append(response.Items, item)
			for _, dc := range item.DisconnectedDevices {
				response.DisconnectedList = append(response.DisconnectedList, dc)
			}
		}
	}

	// Batch analyze all links
	if len(req.Links) > 0 {
		linkItems, err := analyzeLinksImpactBatch(ctx, session, req.Links)
		if err != nil {
			response.Error = fmt.Sprintf("failed to analyze links impact: %v", err)
			writeJSON(w, response)
			return
		}
		for _, item := range linkItems {
			response.Items = append(response.Items, item)
			for _, dc := range item.DisconnectedDevices {
				response.DisconnectedList = append(response.DisconnectedList, dc)
			}
		}
		// Track link endpoints for path analysis
		for _, linkPK := range req.Links {
			endpoints := getLinkEndpoints(ctx, linkPK)
			if endpoints != "" {
				offlineLinkEndpoints[endpoints] = true
			}
		}
	}

	// Sort items by impact (ascending) for recommended order
	sortedItems := make([]MaintenanceItem, len(response.Items))
	copy(sortedItems, response.Items)

	// Simple bubble sort by impact (least impactful first)
	for i := 0; i < len(sortedItems)-1; i++ {
		for j := 0; j < len(sortedItems)-i-1; j++ {
			if sortedItems[j].Impact > sortedItems[j+1].Impact {
				sortedItems[j], sortedItems[j+1] = sortedItems[j+1], sortedItems[j]
			}
		}
	}

	// Build recommended order
	for _, item := range sortedItems {
		response.RecommendedOrder = append(response.RecommendedOrder, item.PK)
	}

	// Calculate total impact
	for _, item := range response.Items {
		response.TotalImpact += item.Impact
		response.TotalDisconnected += item.Disconnected
	}

	// Compute affected paths with before/after routing metrics
	response.AffectedPaths = computeAffectedPathsFast(ctx, session, offlineDevicePKs, 50)

	// Compute affected metro pairs - simplified
	response.AffectedMetros = computeAffectedMetrosFast(ctx, session, offlineDevicePKs)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Maintenance impact analyzed %d devices, %d links in %v",
		len(req.Devices), len(req.Links), duration)

	writeJSON(w, response)
}

// getLinkEndpoints returns "sourcePK:targetPK" for a link PK
func getLinkEndpoints(ctx context.Context, linkPK string) string {
	query := `SELECT side_a_pk, side_z_pk FROM dz_links_current WHERE pk = $1`
	var sideA, sideZ string
	if err := config.DB.QueryRow(ctx, query, linkPK).Scan(&sideA, &sideZ); err != nil {
		return ""
	}
	if sideA == "" || sideZ == "" {
		return ""
	}
	return sideA + ":" + sideZ
}

// computeAffectedPaths finds paths that would be affected by the maintenance
// and computes alternate routes with before/after hops and metrics
func computeAffectedPaths(ctx context.Context, session neo4j.Session,
	offlineDevices map[string]bool, offlineLinks map[string]bool, limit int) []MaintenanceAffectedPath {

	result := []MaintenanceAffectedPath{}

	offlineDevicePKs := make([]string, 0, len(offlineDevices))
	for pk := range offlineDevices {
		offlineDevicePKs = append(offlineDevicePKs, pk)
	}

	// If no offline devices, skip the path computation
	if len(offlineDevicePKs) == 0 {
		return result
	}

	// Find paths that go through offline devices, compute before/after metrics
	// This query finds the original shortest path, then tries to find an alternate
	cypher := `
		// First, find device pairs where the shortest path goes through an offline device
		MATCH (source:Device), (target:Device)
		WHERE source.isis_system_id IS NOT NULL
		  AND target.isis_system_id IS NOT NULL
		  AND source.pk < target.pk
		  AND NOT source.pk IN $offlineDevicePKs
		  AND NOT target.pk IN $offlineDevicePKs

		// Find the current shortest path
		MATCH originalPath = shortestPath((source)-[:ISIS_ADJACENT*]-(target))
		WHERE any(n IN nodes(originalPath) WHERE n.pk IN $offlineDevicePKs)

		// Calculate original path metrics
		WITH source, target, originalPath,
		     length(originalPath) AS hopsBefore,
		     reduce(m = 0, r IN relationships(originalPath) | m + coalesce(r.metric, 10)) AS metricBefore

		LIMIT $limit

		// Get metro info
		OPTIONAL MATCH (source)-[:LOCATED_IN]->(sm:Metro)
		OPTIONAL MATCH (target)-[:LOCATED_IN]->(tm:Metro)

		// Return with source/target PKs for alternate path lookup
		RETURN source.pk AS sourcePK, source.code AS sourceCode,
		       target.pk AS targetPK, target.code AS targetCode,
		       COALESCE(sm.code, 'unknown') AS sourceMetro,
		       COALESCE(tm.code, 'unknown') AS targetMetro,
		       hopsBefore, metricBefore
	`

	records, err := session.Run(ctx, cypher, map[string]interface{}{
		"offlineDevicePKs": offlineDevicePKs,
		"limit":            limit,
	})
	if err != nil {
		log.Printf("Error computing affected paths: %v", err)
		return result
	}

	// Collect paths that need alternate route computation
	type pathInfo struct {
		sourcePK     string
		targetPK     string
		sourceCode   string
		targetCode   string
		sourceMetro  string
		targetMetro  string
		hopsBefore   int
		metricBefore int
	}
	paths := []pathInfo{}

	for records.Next(ctx) {
		record := records.Record()
		sourcePK, _ := record.Get("sourcePK")
		targetPK, _ := record.Get("targetPK")
		sourceCode, _ := record.Get("sourceCode")
		targetCode, _ := record.Get("targetCode")
		sourceMetro, _ := record.Get("sourceMetro")
		targetMetro, _ := record.Get("targetMetro")
		hopsBefore, _ := record.Get("hopsBefore")
		metricBefore, _ := record.Get("metricBefore")

		paths = append(paths, pathInfo{
			sourcePK:     asString(sourcePK),
			targetPK:     asString(targetPK),
			sourceCode:   asString(sourceCode),
			targetCode:   asString(targetCode),
			sourceMetro:  asString(sourceMetro),
			targetMetro:  asString(targetMetro),
			hopsBefore:   int(asInt64(hopsBefore)),
			metricBefore: int(asInt64(metricBefore)),
		})
	}

	// For each affected path, try to find an alternate route
	for _, p := range paths {
		affectedPath := MaintenanceAffectedPath{
			Source:       p.sourceCode,
			Target:       p.targetCode,
			SourceMetro:  p.sourceMetro,
			TargetMetro:  p.targetMetro,
			HopsBefore:   p.hopsBefore,
			MetricBefore: p.metricBefore,
			HopsAfter:    -1,
			MetricAfter:  -1,
			Status:       "disconnected",
		}

		// Try to find alternate path avoiding offline devices
		altCypher := `
			MATCH (source:Device {pk: $sourcePK}), (target:Device {pk: $targetPK})

			// Find shortest path that avoids offline devices
			MATCH altPath = shortestPath((source)-[:ISIS_ADJACENT*]-(target))
			WHERE none(n IN nodes(altPath) WHERE n.pk IN $offlineDevicePKs)

			WITH altPath, length(altPath) AS hopsAfter,
			     reduce(m = 0, r IN relationships(altPath) | m + coalesce(r.metric, 10)) AS metricAfter

			RETURN hopsAfter, metricAfter
			LIMIT 1
		`

		altRecords, err := session.Run(ctx, altCypher, map[string]interface{}{
			"sourcePK":         p.sourcePK,
			"targetPK":         p.targetPK,
			"offlineDevicePKs": offlineDevicePKs,
		})
		if err == nil && altRecords.Next(ctx) {
			record := altRecords.Record()
			hopsAfter, _ := record.Get("hopsAfter")
			metricAfter, _ := record.Get("metricAfter")

			affectedPath.HopsAfter = int(asInt64(hopsAfter))
			affectedPath.MetricAfter = int(asInt64(metricAfter))

			// Determine status based on degradation
			hopIncrease := affectedPath.HopsAfter - affectedPath.HopsBefore
			metricIncrease := affectedPath.MetricAfter - affectedPath.MetricBefore

			if hopIncrease > 2 || metricIncrease > 100 {
				affectedPath.Status = "degraded"
			} else {
				affectedPath.Status = "rerouted"
			}
		}

		result = append(result, affectedPath)
	}

	return result
}


// analyzeDevicesImpactBatch computes the impact of taking multiple devices offline in a single query
func analyzeDevicesImpactBatch(ctx context.Context, session neo4j.Session, devicePKs []string) []MaintenanceItem {
	items := make([]MaintenanceItem, 0, len(devicePKs))

	// Single query to get all device info, neighbor counts, and leaf neighbors
	cypher := `
		UNWIND $devicePKs AS devicePK
		MATCH (d:Device {pk: devicePK})
		WHERE d.isis_system_id IS NOT NULL

		// Get device code
		WITH d, devicePK

		// Count neighbors (for impact estimate)
		OPTIONAL MATCH (d)-[:ISIS_ADJACENT]-(neighbor:Device)
		WHERE neighbor.isis_system_id IS NOT NULL
		WITH d, devicePK, count(DISTINCT neighbor) AS neighborCount

		// Find leaf neighbors (degree 1) that would be disconnected
		OPTIONAL MATCH (d)-[:ISIS_ADJACENT]-(leafNeighbor:Device)
		WHERE leafNeighbor.isis_system_id IS NOT NULL
		WITH d, devicePK, neighborCount, leafNeighbor
		OPTIONAL MATCH (leafNeighbor)-[:ISIS_ADJACENT]-(leafNeighborNeighbor:Device)
		WHERE leafNeighborNeighbor.isis_system_id IS NOT NULL
		WITH d, devicePK, neighborCount, leafNeighbor, count(DISTINCT leafNeighborNeighbor) AS leafNeighborDegree
		WITH d, devicePK, neighborCount,
		     CASE WHEN leafNeighborDegree = 1 THEN leafNeighbor.code ELSE null END AS disconnectedCode

		WITH d.pk AS pk, d.code AS code, neighborCount,
		     collect(disconnectedCode) AS disconnectedCodes

		RETURN pk, code, neighborCount,
		       [x IN disconnectedCodes WHERE x IS NOT NULL] AS disconnectedDevices
	`

	result, err := session.Run(ctx, cypher, map[string]interface{}{
		"devicePKs": devicePKs,
	})
	if err != nil {
		log.Printf("Batch device impact query error: %v", err)
		// Fallback to individual queries
		for _, pk := range devicePKs {
			items = append(items, analyzeDeviceImpact(ctx, session, pk))
		}
		return items
	}

	resultMap := make(map[string]MaintenanceItem)
	for result.Next(ctx) {
		record := result.Record()
		pk, _ := record.Get("pk")
		code, _ := record.Get("code")
		neighborCount, _ := record.Get("neighborCount")
		disconnectedDevices, _ := record.Get("disconnectedDevices")

		disconnectedList := []string{}
		if arr, ok := disconnectedDevices.([]interface{}); ok {
			for _, v := range arr {
				if s := asString(v); s != "" {
					disconnectedList = append(disconnectedList, s)
				}
			}
		}

		// Impact estimate: neighbor count squared (rough approximation of paths through this device)
		nc := int(asInt64(neighborCount))
		impact := nc * nc

		item := MaintenanceItem{
			Type:                "device",
			PK:                  asString(pk),
			Code:                asString(code),
			Impact:              impact,
			Disconnected:        len(disconnectedList),
			CausesPartition:     len(disconnectedList) > 0,
			DisconnectedDevices: disconnectedList,
		}
		resultMap[asString(pk)] = item
	}

	// Return items in the same order as input
	for _, pk := range devicePKs {
		if item, ok := resultMap[pk]; ok {
			items = append(items, item)
		} else {
			// Device not found in graph
			items = append(items, MaintenanceItem{
				Type: "device",
				PK:   pk,
				Code: "Unknown device",
			})
		}
	}

	return items
}

// analyzeLinksImpactBatch computes the impact of taking multiple links offline
func analyzeLinksImpactBatch(ctx context.Context, session neo4j.Session, linkPKs []string) ([]MaintenanceItem, error) {
	items := make([]MaintenanceItem, 0, len(linkPKs))

	// First, batch lookup links from ClickHouse
	if len(linkPKs) == 0 {
		return items, nil
	}

	// Build placeholders for ClickHouse query
	linkQuery := `
		SELECT
			l.pk,
			l.code,
			COALESCE(l.side_a_pk, '') as side_a_pk,
			COALESCE(l.side_z_pk, '') as side_z_pk,
			COALESCE(da.code, '') as side_a_code,
			COALESCE(dz.code, '') as side_z_code
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		WHERE l.pk IN ($1)
	`

	// For ClickHouse we need to pass as a tuple
	rows, err := config.DB.Query(ctx, linkQuery, linkPKs)
	if err != nil {
		return nil, fmt.Errorf("batch link lookup error: %w", err)
	}
	defer rows.Close()

	type linkInfo struct {
		pk        string
		code      string
		sideAPK   string
		sideZPK   string
		sideACode string
		sideZCode string
	}
	linkMap := make(map[string]linkInfo)

	for rows.Next() {
		var li linkInfo
		if err := rows.Scan(&li.pk, &li.code, &li.sideAPK, &li.sideZPK, &li.sideACode, &li.sideZCode); err != nil {
			return nil, fmt.Errorf("failed to scan link info row: %w", err)
		}
		linkMap[li.pk] = li
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate link info rows: %w", err)
	}

	// Now batch query Neo4j for degree information
	var linkEndpoints []map[string]string
	for _, pk := range linkPKs {
		if li, ok := linkMap[pk]; ok && li.sideAPK != "" && li.sideZPK != "" {
			linkEndpoints = append(linkEndpoints, map[string]string{
				"pk":      pk,
				"sourceA": li.sideAPK,
				"sourceZ": li.sideZPK,
			})
		}
	}

	// Single Neo4j query for all link endpoints
	degreeCypher := `
		UNWIND $links AS link
		MATCH (s:Device {pk: link.sourceA}), (t:Device {pk: link.sourceZ})
		WHERE s.isis_system_id IS NOT NULL AND t.isis_system_id IS NOT NULL

		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sn:Device) WHERE sn.isis_system_id IS NOT NULL
		WITH link, s, t, count(DISTINCT sn) AS sourceDegree
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tn:Device) WHERE tn.isis_system_id IS NOT NULL
		WITH link, s, t, sourceDegree, count(DISTINCT tn) AS targetDegree

		RETURN link.pk AS pk, s.code AS sourceCode, t.code AS targetCode, sourceDegree, targetDegree
	`

	degreeMap := make(map[string]struct {
		sourceCode   string
		targetCode   string
		sourceDegree int
		targetDegree int
	})

	if len(linkEndpoints) > 0 {
		result, err := session.Run(ctx, degreeCypher, map[string]interface{}{
			"links": linkEndpoints,
		})
		if err == nil {
			for result.Next(ctx) {
				record := result.Record()
				pk, _ := record.Get("pk")
				sourceCode, _ := record.Get("sourceCode")
				targetCode, _ := record.Get("targetCode")
				sourceDegree, _ := record.Get("sourceDegree")
				targetDegree, _ := record.Get("targetDegree")

				degreeMap[asString(pk)] = struct {
					sourceCode   string
					targetCode   string
					sourceDegree int
					targetDegree int
				}{
					sourceCode:   asString(sourceCode),
					targetCode:   asString(targetCode),
					sourceDegree: int(asInt64(sourceDegree)),
					targetDegree: int(asInt64(targetDegree)),
				}
			}
		}
	}

	// Build items in order
	for _, pk := range linkPKs {
		li, hasLink := linkMap[pk]
		if !hasLink {
			items = append(items, MaintenanceItem{
				Type: "link",
				PK:   pk,
				Code: "Link not found",
			})
			continue
		}

		if li.sideAPK == "" || li.sideZPK == "" {
			items = append(items, MaintenanceItem{
				Type: "link",
				PK:   pk,
				Code: li.code + " (missing endpoints)",
			})
			continue
		}

		item := MaintenanceItem{
			Type: "link",
			PK:   pk,
			Code: li.sideACode + " - " + li.sideZCode,
		}

		if deg, ok := degreeMap[pk]; ok {
			// If either has degree 1, this link is critical
			if deg.sourceDegree == 1 || deg.targetDegree == 1 {
				item.CausesPartition = true
				if deg.sourceDegree == 1 {
					item.DisconnectedDevices = append(item.DisconnectedDevices, deg.sourceCode)
					item.Disconnected++
				}
				if deg.targetDegree == 1 {
					item.DisconnectedDevices = append(item.DisconnectedDevices, deg.targetCode)
					item.Disconnected++
				}
			}
			// Impact estimate: product of (degree-1) for each side (paths that would need rerouting)
			srcNeighbors := deg.sourceDegree - 1
			tgtNeighbors := deg.targetDegree - 1
			if srcNeighbors < 0 {
				srcNeighbors = 0
			}
			if tgtNeighbors < 0 {
				tgtNeighbors = 0
			}
			item.Impact = srcNeighbors * tgtNeighbors
		}

		items = append(items, item)
	}

	return items, nil
}

// computeAffectedPathsFast finds all paths affected by taking devices offline
// Returns paths that will be rerouted (with before/after metrics) and paths that will be disconnected
func computeAffectedPathsFast(ctx context.Context, session neo4j.Session,
	offlineDevices map[string]bool, limit int) []MaintenanceAffectedPath {

	result := []MaintenanceAffectedPath{}

	offlineDevicePKs := make([]string, 0, len(offlineDevices))
	for pk := range offlineDevices {
		offlineDevicePKs = append(offlineDevicePKs, pk)
	}

	if len(offlineDevicePKs) == 0 {
		return result
	}

	// Step 1: Find all neighbors of offline devices (these are the directly affected connections)
	// For each neighbor pair (neighbor of offline device A, neighbor of offline device B or other device),
	// compute the current shortest path and the alternate path avoiding offline devices
	cypher := `
		// Find all ISIS neighbors of offline devices
		MATCH (offline:Device)-[:ISIS_ADJACENT]-(neighbor:Device)
		WHERE offline.pk IN $offlineDevicePKs
		  AND offline.isis_system_id IS NOT NULL
		  AND neighbor.isis_system_id IS NOT NULL
		  AND NOT neighbor.pk IN $offlineDevicePKs

		WITH DISTINCT neighbor

		// For each neighbor, find paths to other devices that currently go through an offline device
		MATCH (neighbor)-[:ISIS_ADJACENT*1..2]-(other:Device)
		WHERE other.isis_system_id IS NOT NULL
		  AND other.pk <> neighbor.pk
		  AND NOT other.pk IN $offlineDevicePKs

		WITH DISTINCT neighbor, other
		WHERE neighbor.pk < other.pk  // Avoid duplicates

		// Get current shortest path
		MATCH currentPath = shortestPath((neighbor)-[:ISIS_ADJACENT*]-(other))
		WITH neighbor, other, currentPath,
		     length(currentPath) AS currentHops,
		     reduce(m = 0, r IN relationships(currentPath) | m + coalesce(r.metric, 10)) AS currentMetric,
		     any(n IN nodes(currentPath) WHERE n.pk IN $offlineDevicePKs) AS goesThruOffline

		WHERE goesThruOffline = true

		// Get metro info
		OPTIONAL MATCH (neighbor)-[:LOCATED_IN]->(nm:Metro)
		OPTIONAL MATCH (other)-[:LOCATED_IN]->(om:Metro)

		RETURN neighbor.pk AS sourcePK, neighbor.code AS sourceCode,
		       other.pk AS targetPK, other.code AS targetCode,
		       COALESCE(nm.code, 'unknown') AS sourceMetro,
		       COALESCE(om.code, 'unknown') AS targetMetro,
		       currentHops, currentMetric
		LIMIT $limit
	`

	records, err := session.Run(ctx, cypher, map[string]interface{}{
		"offlineDevicePKs": offlineDevicePKs,
		"limit":            limit * 2, // Get more candidates, we'll filter
	})
	if err != nil {
		log.Printf("Error computing affected paths: %v", err)
		return result
	}

	// Collect paths that need alternate route computation
	type pathCandidate struct {
		sourcePK      string
		sourceCode    string
		targetPK      string
		targetCode    string
		sourceMetro   string
		targetMetro   string
		currentHops   int
		currentMetric int
	}
	candidates := []pathCandidate{}

	for records.Next(ctx) {
		record := records.Record()
		sourcePK, _ := record.Get("sourcePK")
		sourceCode, _ := record.Get("sourceCode")
		targetPK, _ := record.Get("targetPK")
		targetCode, _ := record.Get("targetCode")
		sourceMetro, _ := record.Get("sourceMetro")
		targetMetro, _ := record.Get("targetMetro")
		currentHops, _ := record.Get("currentHops")
		currentMetric, _ := record.Get("currentMetric")

		candidates = append(candidates, pathCandidate{
			sourcePK:      asString(sourcePK),
			sourceCode:    asString(sourceCode),
			targetPK:      asString(targetPK),
			targetCode:    asString(targetCode),
			sourceMetro:   asString(sourceMetro),
			targetMetro:   asString(targetMetro),
			currentHops:   int(asInt64(currentHops)),
			currentMetric: int(asInt64(currentMetric)),
		})
	}

	// Step 2: For each candidate, find alternate path avoiding offline devices
	for _, c := range candidates {
		if len(result) >= limit {
			break
		}

		path := MaintenanceAffectedPath{
			Source:       c.sourceCode,
			Target:       c.targetCode,
			SourceMetro:  c.sourceMetro,
			TargetMetro:  c.targetMetro,
			HopsBefore:   c.currentHops,
			MetricBefore: c.currentMetric,
			HopsAfter:    -1,
			MetricAfter:  -1,
			Status:       "disconnected",
		}

		// Try to find alternate path
		altCypher := `
			MATCH (source:Device {pk: $sourcePK}), (target:Device {pk: $targetPK})
			MATCH altPath = shortestPath((source)-[:ISIS_ADJACENT*]-(target))
			WHERE none(n IN nodes(altPath) WHERE n.pk IN $offlineDevicePKs)
			WITH altPath, length(altPath) AS altHops,
			     reduce(m = 0, r IN relationships(altPath) | m + coalesce(r.metric, 10)) AS altMetric
			RETURN altHops, altMetric
			LIMIT 1
		`

		altResult, err := session.Run(ctx, altCypher, map[string]interface{}{
			"sourcePK":         c.sourcePK,
			"targetPK":         c.targetPK,
			"offlineDevicePKs": offlineDevicePKs,
		})
		if err == nil && altResult.Next(ctx) {
			record := altResult.Record()
			altHops, _ := record.Get("altHops")
			altMetric, _ := record.Get("altMetric")

			path.HopsAfter = int(asInt64(altHops))
			path.MetricAfter = int(asInt64(altMetric))

			// Classify based on degradation
			hopIncrease := path.HopsAfter - path.HopsBefore
			metricIncrease := path.MetricAfter - path.MetricBefore
			if hopIncrease > 2 || metricIncrease > 50 {
				path.Status = "degraded"
			} else {
				path.Status = "rerouted"
			}
		}

		result = append(result, path)
	}

	return result
}

// computeAffectedMetrosFast computes affected metro pairs with specific link details
func computeAffectedMetrosFast(ctx context.Context, session neo4j.Session,
	offlineDevices map[string]bool) []AffectedMetroPair {

	result := []AffectedMetroPair{}

	offlineDevicePKs := make([]string, 0, len(offlineDevices))
	for pk := range offlineDevices {
		offlineDevicePKs = append(offlineDevicePKs, pk)
	}

	if len(offlineDevicePKs) == 0 {
		return result
	}

	// Query: find ISIS adjacencies that involve offline devices, grouped by metro pair
	// Returns the specific device pairs affected
	cypher := `
		MATCH (d1:Device)-[:ISIS_ADJACENT]-(d2:Device)
		WHERE d1.pk IN $offlineDevicePKs
		  AND d1.isis_system_id IS NOT NULL
		  AND d2.isis_system_id IS NOT NULL
		  AND NOT d2.pk IN $offlineDevicePKs

		// Get metro info for both devices
		OPTIONAL MATCH (d1)-[:LOCATED_IN]->(m1:Metro)
		OPTIONAL MATCH (d2)-[:LOCATED_IN]->(m2:Metro)

		WITH COALESCE(m1.code, 'unknown') AS metro1,
		     COALESCE(m2.code, 'unknown') AS metro2,
		     d1.code AS device1,
		     d2.code AS device2,
		     d1.pk AS d1pk

		// Return individual links grouped by metro pair
		RETURN metro1, metro2, device1, device2, d1pk
		ORDER BY metro1, metro2, device1
		LIMIT 50
	`

	records, err := session.Run(ctx, cypher, map[string]interface{}{
		"offlineDevicePKs": offlineDevicePKs,
	})
	if err != nil {
		log.Printf("Error computing affected metros fast: %v", err)
		return result
	}

	// Group links by metro pair
	type metroPairKey struct {
		metro1, metro2 string
	}
	metroPairs := make(map[metroPairKey]*AffectedMetroPair)

	for records.Next(ctx) {
		record := records.Record()
		metro1, _ := record.Get("metro1")
		metro2, _ := record.Get("metro2")
		device1, _ := record.Get("device1")
		device2, _ := record.Get("device2")

		m1 := asString(metro1)
		m2 := asString(metro2)

		// Normalize key so we don't duplicate metro pairs in different order
		key := metroPairKey{m1, m2}
		if m1 > m2 {
			key = metroPairKey{m2, m1}
		}

		pair, exists := metroPairs[key]
		if !exists {
			pair = &AffectedMetroPair{
				SourceMetro:   key.metro1,
				TargetMetro:   key.metro2,
				AffectedLinks: []AffectedLink{},
				Status:        "reduced",
			}
			metroPairs[key] = pair
		}

		// Add the affected link
		link := AffectedLink{
			SourceDevice: asString(device1),
			TargetDevice: asString(device2),
			Status:       "offline", // The source device is going offline
		}
		pair.AffectedLinks = append(pair.AffectedLinks, link)
	}

	// Convert map to slice
	for _, pair := range metroPairs {
		result = append(result, *pair)
	}

	return result
}

// analyzeDeviceImpact computes the impact of taking a single device offline
func analyzeDeviceImpact(ctx context.Context, session neo4j.Session, devicePK string) MaintenanceItem {
	item := MaintenanceItem{
		Type: "device",
		PK:   devicePK,
	}

	// Get device code
	codeCypher := `
		MATCH (d:Device {pk: $pk})
		RETURN d.code AS code
	`
	codeResult, err := session.Run(ctx, codeCypher, map[string]interface{}{"pk": devicePK})
	if err == nil {
		if record, err := codeResult.Single(ctx); err == nil {
			if code, ok := record.Get("code"); ok {
				item.Code = asString(code)
			}
		}
	}

	// Count paths that go through this device
	pathsCypher := `
		MATCH (d:Device {pk: $pk})
		WHERE d.isis_system_id IS NOT NULL
		OPTIONAL MATCH (other:Device)
		WHERE other.isis_system_id IS NOT NULL AND other.pk <> d.pk
		OPTIONAL MATCH path = shortestPath((other)-[:ISIS_ADJACENT*]-(d))
		WITH d, count(path) AS pathCount
		RETURN pathCount
	`
	pathsResult, err := session.Run(ctx, pathsCypher, map[string]interface{}{"pk": devicePK})
	if err == nil {
		if record, err := pathsResult.Single(ctx); err == nil {
			if pathCount, ok := record.Get("pathCount"); ok {
				item.Impact = int(asInt64(pathCount))
			}
		}
	}

	// Check if this device is critical (would disconnect others)
	// A device is critical if any of its neighbors have degree 1 (only connected to this device)
	criticalCypher := `
		MATCH (d:Device {pk: $pk})-[:ISIS_ADJACENT]-(neighbor:Device)
		WHERE d.isis_system_id IS NOT NULL AND neighbor.isis_system_id IS NOT NULL
		WITH neighbor
		MATCH (neighbor)-[:ISIS_ADJACENT]-(any:Device)
		WHERE any.isis_system_id IS NOT NULL
		WITH neighbor, count(DISTINCT any) AS degree
		WHERE degree = 1
		RETURN neighbor.code AS disconnectedCode
	`
	criticalResult, err := session.Run(ctx, criticalCypher, map[string]interface{}{"pk": devicePK})
	if err == nil {
		for criticalResult.Next(ctx) {
			record := criticalResult.Record()
			if code, ok := record.Get("disconnectedCode"); ok {
				item.DisconnectedDevices = append(item.DisconnectedDevices, asString(code))
				item.Disconnected++
			}
		}
		item.CausesPartition = item.Disconnected > 0
	}

	return item
}

// analyzeLinkImpact computes the impact of taking a single link offline
func analyzeLinkImpact(ctx context.Context, session neo4j.Session, linkPK string) MaintenanceItem {
	item := MaintenanceItem{
		Type: "link",
		PK:   linkPK,
	}

	// Look up link from ClickHouse to get side_a_pk and side_z_pk
	linkQuery := `
		SELECT
			l.code,
			COALESCE(l.side_a_pk, '') as side_a_pk,
			COALESCE(l.side_z_pk, '') as side_z_pk,
			COALESCE(da.code, '') as side_a_code,
			COALESCE(dz.code, '') as side_z_code
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		WHERE l.pk = $1
	`
	var linkCode, sideAPK, sideZPK, sideACode, sideZCode string
	if err := config.DB.QueryRow(ctx, linkQuery, linkPK).Scan(&linkCode, &sideAPK, &sideZPK, &sideACode, &sideZCode); err != nil {
		item.Code = "Link not found"
		return item
	}

	if sideAPK == "" || sideZPK == "" {
		item.Code = linkCode + " (missing endpoints)"
		return item
	}

	item.Code = sideACode + " - " + sideZCode
	sourcePK, targetPK := sideAPK, sideZPK

	// Check if removing this link would disconnect devices
	// If either endpoint has degree 1, removing the link disconnects that device
	degreeCypher := `
		MATCH (s:Device {pk: $sourcePK}), (t:Device {pk: $targetPK})
		WHERE s.isis_system_id IS NOT NULL AND t.isis_system_id IS NOT NULL
		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sn:Device) WHERE sn.isis_system_id IS NOT NULL
		WITH s, t, count(DISTINCT sn) AS sourceDegree
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tn:Device) WHERE tn.isis_system_id IS NOT NULL
		WITH s, t, sourceDegree, count(DISTINCT tn) AS targetDegree
		RETURN s.code AS sourceCode, t.code AS targetCode, sourceDegree, targetDegree
	`
	degreeResult, err := session.Run(ctx, degreeCypher, map[string]interface{}{
		"sourcePK": sourcePK,
		"targetPK": targetPK,
	})
	if err == nil {
		if record, err := degreeResult.Single(ctx); err == nil {
			sourceCode, _ := record.Get("sourceCode")
			targetCode, _ := record.Get("targetCode")
			sourceDegree, _ := record.Get("sourceDegree")
			targetDegree, _ := record.Get("targetDegree")
			sDeg := int(asInt64(sourceDegree))
			tDeg := int(asInt64(targetDegree))

			// If either has degree 1, this link is critical
			if sDeg == 1 || tDeg == 1 {
				item.CausesPartition = true
				if sDeg == 1 {
					item.DisconnectedDevices = append(item.DisconnectedDevices, asString(sourceCode))
					item.Disconnected++
				}
				if tDeg == 1 {
					item.DisconnectedDevices = append(item.DisconnectedDevices, asString(targetCode))
					item.Disconnected++
				}
			}
		}
	}

	// Count paths that use this link
	// We count device pairs where the shortest path goes through this link
	pathsCypher := `
		MATCH (s:Device {pk: $sourcePK})-[:ISIS_ADJACENT]-(t:Device {pk: $targetPK})
		WHERE s.isis_system_id IS NOT NULL AND t.isis_system_id IS NOT NULL
		// Get neighbors of source (excluding target)
		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sNeighbor:Device)
		WHERE sNeighbor.isis_system_id IS NOT NULL AND sNeighbor.pk <> t.pk
		WITH s, t, collect(DISTINCT sNeighbor.pk) AS sourceNeighbors
		// Get neighbors of target (excluding source)
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tNeighbor:Device)
		WHERE tNeighbor.isis_system_id IS NOT NULL AND tNeighbor.pk <> s.pk
		WITH sourceNeighbors, collect(DISTINCT tNeighbor.pk) AS targetNeighbors
		// Rough estimate: paths affected = sourceNeighbors * targetNeighbors
		RETURN size(sourceNeighbors) * size(targetNeighbors) AS affectedPaths
	`
	pathsResult, err := session.Run(ctx, pathsCypher, map[string]interface{}{
		"sourcePK": sourcePK,
		"targetPK": targetPK,
	})
	if err == nil {
		if record, err := pathsResult.Single(ctx); err == nil {
			if affectedPaths, ok := record.Get("affectedPaths"); ok {
				item.Impact = int(asInt64(affectedPaths))
			}
		}
	}

	return item
}
