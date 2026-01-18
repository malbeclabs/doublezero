package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
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

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := ISISTopologyResponse{
		Nodes: []ISISNode{},
		Edges: []ISISEdge{},
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

	deviceResult, err := session.Run(ctx, deviceCypher, nil)
	if err != nil {
		log.Printf("ISIS topology device query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	deviceRecords, err := deviceResult.Collect(ctx)
	if err != nil {
		log.Printf("ISIS topology device collect error: %v", err)
		response.Error = err.Error()
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

	adjResult, err := session.Run(ctx, adjCypher, nil)
	if err != nil {
		log.Printf("ISIS topology adjacency query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	adjRecords, err := adjResult.Collect(ctx)
	if err != nil {
		log.Printf("ISIS topology adjacency collect error: %v", err)
		response.Error = err.Error()
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

// FailureImpactResponse is the response for the failure impact endpoint
type FailureImpactResponse struct {
	DevicePK           string         `json:"devicePK"`
	DeviceCode         string         `json:"deviceCode"`
	UnreachableDevices []ImpactDevice `json:"unreachableDevices"`
	UnreachableCount   int            `json:"unreachableCount"`
	Error              string         `json:"error,omitempty"`
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

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	writeJSON(w, response)
}

// MultiPathHop represents a hop in a path with edge metric information
type MultiPathHop struct {
	DevicePK   string `json:"devicePK"`
	DeviceCode string `json:"deviceCode"`
	Status     string `json:"status"`
	DeviceType string `json:"deviceType"`
	EdgeMetric uint32 `json:"edgeMetric,omitempty"` // metric to reach this hop from previous
}

// SinglePath represents one path in a multi-path response
type SinglePath struct {
	Path        []MultiPathHop `json:"path"`
	TotalMetric uint32         `json:"totalMetric"`
	HopCount    int            `json:"hopCount"`
}

// MultiPathResponse is the response for the K-shortest paths endpoint
type MultiPathResponse struct {
	Paths []SinglePath `json:"paths"`
	From  string       `json:"from"`
	To    string       `json:"to"`
	Error string       `json:"error,omitempty"`
}

// GetISISPaths finds K-shortest paths between two devices
func GetISISPaths(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	fromPK := r.URL.Query().Get("from")
	toPK := r.URL.Query().Get("to")
	kStr := r.URL.Query().Get("k")

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

	// Find K-shortest paths by total metric
	// Uses allShortestPaths to get multiple equal-cost paths, plus some longer alternatives
	cypher := `
		MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})

		// First get all shortest paths (equal cost)
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

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)
	log.Printf("ISIS multi-path query returned %d paths in %v", len(response.Paths), duration)

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
	FromMetroPK   string `json:"fromMetroPK"`
	FromMetroCode string `json:"fromMetroCode"`
	FromMetroName string `json:"fromMetroName"`
	ToMetroPK     string `json:"toMetroPK"`
	ToMetroCode   string `json:"toMetroCode"`
	ToMetroName   string `json:"toMetroName"`
	PathCount     int    `json:"pathCount"`
	MinHops       int    `json:"minHops"`
	MinMetric     int64  `json:"minMetric"`
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

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := MetroConnectivityResponse{
		Metros:       []MetroInfo{},
		Connectivity: []MetroConnectivity{},
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

	metroResult, err := session.Run(ctx, metroCypher, nil)
	if err != nil {
		log.Printf("Metro connectivity metro query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	metroRecords, err := metroResult.Collect(ctx)
	if err != nil {
		log.Printf("Metro connectivity metro collect error: %v", err)
		response.Error = err.Error()
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
		     reduce(total = 0, r IN relationships(path) | total + coalesce(r.metric, 0)) AS metric
		WITH m1, m2, min(hops) AS minHops, min(metric) AS minMetric, count(*) AS pathCount
		RETURN m1.pk AS fromPK, m1.code AS fromCode, m1.name AS fromName,
		       m2.pk AS toPK, m2.code AS toCode, m2.name AS toName,
		       minHops, minMetric, pathCount
		ORDER BY fromCode, toCode
	`

	connResult, err := session.Run(ctx, connectivityCypher, nil)
	if err != nil {
		log.Printf("Metro connectivity query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	connRecords, err := connResult.Collect(ctx)
	if err != nil {
		log.Printf("Metro connectivity collect error: %v", err)
		response.Error = err.Error()
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

		// Add both directions (matrix is symmetric)
		conn := MetroConnectivity{
			FromMetroPK:   asString(fromPK),
			FromMetroCode: asString(fromCode),
			FromMetroName: asString(fromName),
			ToMetroPK:     asString(toPK),
			ToMetroCode:   asString(toCode),
			ToMetroName:   asString(toName),
			PathCount:     int(asInt64(pathCount)),
			MinHops:       int(asInt64(minHops)),
			MinMetric:     asInt64(minMetric),
		}
		response.Connectivity = append(response.Connectivity, conn)

		// Add reverse direction
		connReverse := MetroConnectivity{
			FromMetroPK:   asString(toPK),
			FromMetroCode: asString(toCode),
			FromMetroName: asString(toName),
			ToMetroPK:     asString(fromPK),
			ToMetroCode:   asString(fromCode),
			ToMetroName:   asString(fromName),
			PathCount:     int(asInt64(pathCount)),
			MinHops:       int(asInt64(minHops)),
			MinMetric:     asInt64(minMetric),
		}
		response.Connectivity = append(response.Connectivity, connReverse)
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil) // Reuse existing metric for now

	log.Printf("Metro connectivity returned %d metros, %d connections in %v",
		len(response.Metros), len(response.Connectivity), duration)

	writeJSON(w, response)
}
