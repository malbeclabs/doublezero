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
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
)

// SimulateLinkRemovalResponse is the response for simulating link removal
type SimulateLinkRemovalResponse struct {
	SourcePK             string             `json:"sourcePK"`
	SourceCode           string             `json:"sourceCode"`
	TargetPK             string             `json:"targetPK"`
	TargetCode           string             `json:"targetCode"`
	DisconnectedDevices  []ImpactDevice     `json:"disconnectedDevices"`
	DisconnectedCount    int                `json:"disconnectedCount"`
	AffectedPaths        []AffectedPath     `json:"affectedPaths"`
	AffectedPathCount    int                `json:"affectedPathCount"`
	CausesPartition      bool               `json:"causesPartition"`
	Error                string             `json:"error,omitempty"`
}

// AffectedPath represents a path that would be affected by link removal
type AffectedPath struct {
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

// GetSimulateLinkRemoval simulates removing a link and shows the impact
func GetSimulateLinkRemoval(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	sourcePK := r.URL.Query().Get("sourcePK")
	targetPK := r.URL.Query().Get("targetPK")

	if sourcePK == "" || targetPK == "" {
		writeJSON(w, SimulateLinkRemovalResponse{Error: "sourcePK and targetPK parameters are required"})
		return
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := SimulateLinkRemovalResponse{
		SourcePK:            sourcePK,
		TargetPK:            targetPK,
		DisconnectedDevices: []ImpactDevice{},
		AffectedPaths:       []AffectedPath{},
	}

	// Get device codes
	codesCypher := `
		MATCH (s:Device {pk: $source_pk})
		MATCH (t:Device {pk: $target_pk})
		RETURN s.code AS source_code, t.code AS target_code
	`
	codesResult, err := session.Run(ctx, codesCypher, map[string]any{
		"source_pk": sourcePK,
		"target_pk": targetPK,
	})
	if err != nil {
		log.Printf("Simulate link removal codes query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}
	if codesRecord, err := codesResult.Single(ctx); err == nil {
		sourceCode, _ := codesRecord.Get("source_code")
		targetCode, _ := codesRecord.Get("target_code")
		response.SourceCode = asString(sourceCode)
		response.TargetCode = asString(targetCode)
	}

	// Check if removing this link would disconnect any devices
	// A device becomes disconnected if it has degree 1 (leaf node) - removing its only link disconnects it
	disconnectCypher := `
		MATCH (s:Device {pk: $source_pk}), (t:Device {pk: $target_pk})
		WHERE s.isis_system_id IS NOT NULL AND t.isis_system_id IS NOT NULL

		// Count neighbors of each endpoint
		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sn:Device)
		WHERE sn.isis_system_id IS NOT NULL
		WITH s, t, count(DISTINCT sn) AS sourceDegree
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tn:Device)
		WHERE tn.isis_system_id IS NOT NULL
		WITH s, t, sourceDegree, count(DISTINCT tn) AS targetDegree

		// A partition occurs if one endpoint has degree 1 (it's a leaf node)
		// If both have degree > 1, there must be alternate paths (even if longer)
		WITH s, t, sourceDegree, targetDegree,
		     CASE WHEN sourceDegree = 1 OR targetDegree = 1 THEN true ELSE false END AS causesPartition
		WHERE causesPartition = true

		// Return the device(s) that would be disconnected - the leaf node(s)
		UNWIND CASE
			WHEN sourceDegree = 1 AND targetDegree = 1 THEN [s, t]
			WHEN sourceDegree = 1 THEN [s]
			WHEN targetDegree = 1 THEN [t]
			ELSE []
		END AS d
		RETURN d.pk AS pk, d.code AS code, d.status AS status, d.device_type AS device_type
	`

	disconnectResult, err := session.Run(ctx, disconnectCypher, map[string]any{
		"source_pk": sourcePK,
		"target_pk": targetPK,
	})
	if err != nil {
		log.Printf("Simulate link removal disconnect query error: %v", err)
		response.Error = "failed to query disconnect impact"
	} else {
		disconnectRecords, err := disconnectResult.Collect(ctx)
		if err != nil {
			log.Printf("Simulate link removal disconnect collect error: %v", err)
			response.Error = "failed to query disconnect impact"
		} else {
			log.Printf("Simulate link removal disconnect query returned %d records", len(disconnectRecords))
			for _, record := range disconnectRecords {
				pk, _ := record.Get("pk")
				code, _ := record.Get("code")
				status, _ := record.Get("status")
				deviceType, _ := record.Get("device_type")

				response.DisconnectedDevices = append(response.DisconnectedDevices, ImpactDevice{
					PK:         asString(pk),
					Code:       asString(code),
					Status:     asString(status),
					DeviceType: asString(deviceType),
				})
			}
		}
	}
	response.DisconnectedCount = len(response.DisconnectedDevices)
	response.CausesPartition = response.DisconnectedCount > 0

	// Find affected paths - paths that currently use this link
	// Simplified query: just check direct neighbors of source and target
	affectedCypher := `
		MATCH (src:Device {pk: $source_pk}), (tgt:Device {pk: $target_pk})
		WHERE src.isis_system_id IS NOT NULL AND tgt.isis_system_id IS NOT NULL

		// Get the metric of the link being removed
		OPTIONAL MATCH (src)-[linkRel:ISIS_ADJACENT]-(tgt)
		WITH src, tgt, min(linkRel.metric) AS linkMetric

		// Get immediate neighbors of source with their link metrics
		OPTIONAL MATCH (src)-[srcRel:ISIS_ADJACENT]-(srcNeighbor:Device)
		WHERE srcNeighbor.isis_system_id IS NOT NULL AND srcNeighbor.pk <> tgt.pk
		WITH src, tgt, linkMetric, collect(DISTINCT {device: srcNeighbor, metric: srcRel.metric}) AS srcNeighborsData

		// Get immediate neighbors of target with their link metrics
		OPTIONAL MATCH (tgt)-[tgtRel:ISIS_ADJACENT]-(tgtNeighbor:Device)
		WHERE tgtNeighbor.isis_system_id IS NOT NULL AND tgtNeighbor.pk <> src.pk
		WITH src, tgt, linkMetric, srcNeighborsData, collect(DISTINCT {device: tgtNeighbor, metric: tgtRel.metric}) AS tgtNeighborsData

		// For each source neighbor, check path to target neighbors via this link
		UNWIND CASE WHEN size(srcNeighborsData) > 0 THEN srcNeighborsData ELSE [null] END AS srcData
		UNWIND CASE WHEN size(tgtNeighborsData) > 0 THEN tgtNeighborsData ELSE [null] END AS tgtData
		WITH src, tgt, linkMetric, srcData, tgtData
		WHERE srcData IS NOT NULL AND tgtData IS NOT NULL
		  AND srcData.device.pk <> tgtData.device.pk

		WITH srcData.device AS fromDevice, tgtData.device AS toDevice, src, tgt,
		     3 AS beforeHops,
		     coalesce(srcData.metric, 0) + coalesce(linkMetric, 0) + coalesce(tgtData.metric, 0) AS beforeMetric

		// Check if there's an alternate path not using the link being removed
		OPTIONAL MATCH altPath = shortestPath((fromDevice)-[:ISIS_ADJACENT*]-(toDevice))
		WHERE NONE(r IN relationships(altPath) WHERE
		      (startNode(r).pk = src.pk AND endNode(r).pk = tgt.pk) OR
		      (startNode(r).pk = tgt.pk AND endNode(r).pk = src.pk))
		WITH fromDevice, toDevice, beforeHops, beforeMetric, altPath,
		     CASE WHEN altPath IS NOT NULL THEN length(altPath) ELSE 0 END AS afterHops,
		     CASE WHEN altPath IS NOT NULL
		          THEN reduce(total = 0, r IN relationships(altPath) | total + coalesce(r.metric, 0))
		          ELSE 0 END AS afterMetric

		// Only include paths where the path through the link is actually preferred
		WHERE afterHops = 0 OR (afterHops > 0 AND beforeMetric < afterMetric)

		RETURN fromDevice.pk AS from_pk,
		       fromDevice.code AS from_code,
		       toDevice.pk AS to_pk,
		       toDevice.code AS to_code,
		       beforeHops,
		       beforeMetric,
		       afterHops,
		       afterMetric
		LIMIT 5
	`

	affectedResult, err := session.Run(ctx, affectedCypher, map[string]any{
		"source_pk": sourcePK,
		"target_pk": targetPK,
	})
	if err != nil {
		log.Printf("Simulate link removal affected paths query error: %v", err)
		response.Error = "failed to query affected paths"
	} else {
		affectedRecords, err := affectedResult.Collect(ctx)
		if err != nil {
			log.Printf("Simulate link removal affected paths collect error: %v", err)
			response.Error = "failed to query affected paths"
		} else {
			for _, record := range affectedRecords {
				fromPK, _ := record.Get("from_pk")
				fromCode, _ := record.Get("from_code")
				toPK, _ := record.Get("to_pk")
				toCode, _ := record.Get("to_code")
				beforeHops, _ := record.Get("beforeHops")
				beforeMetric, _ := record.Get("beforeMetric")
				afterHops, _ := record.Get("afterHops")
				afterMetric, _ := record.Get("afterMetric")

				hasAlternate := afterHops != nil && asInt64(afterHops) > 0

				response.AffectedPaths = append(response.AffectedPaths, AffectedPath{
					FromPK:       asString(fromPK),
					FromCode:     asString(fromCode),
					ToPK:         asString(toPK),
					ToCode:       asString(toCode),
					BeforeHops:   int(asInt64(beforeHops)),
					BeforeMetric: uint32(asInt64(beforeMetric)),
					AfterHops:    int(asInt64(afterHops)),
					AfterMetric:  uint32(asInt64(afterMetric)),
					HasAlternate: hasAlternate,
				})
			}
		}
	}
	response.AffectedPathCount = len(response.AffectedPaths)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Simulate link removal: %s -> %s, disconnected=%d, affectedPaths=%d, partition=%v in %v",
		response.SourceCode, response.TargetCode, response.DisconnectedCount, response.AffectedPathCount, response.CausesPartition, duration)

	writeJSON(w, response)
}

// SimulateLinkAdditionResponse is the response for simulating link addition
type SimulateLinkAdditionResponse struct {
	SourcePK          string          `json:"sourcePK"`
	SourceCode        string          `json:"sourceCode"`
	TargetPK          string          `json:"targetPK"`
	TargetCode        string          `json:"targetCode"`
	Metric            uint32          `json:"metric"`
	ImprovedPaths     []ImprovedPath  `json:"improvedPaths"`
	ImprovedPathCount int             `json:"improvedPathCount"`
	RedundancyGains   []RedundancyGain `json:"redundancyGains"`
	RedundancyCount   int             `json:"redundancyCount"`
	Error             string          `json:"error,omitempty"`
}

// ImprovedPath represents a path that would be improved by adding a link
type ImprovedPath struct {
	FromPK       string `json:"fromPK"`
	FromCode     string `json:"fromCode"`
	ToPK         string `json:"toPK"`
	ToCode       string `json:"toCode"`
	BeforeHops   int    `json:"beforeHops"`
	BeforeMetric uint32 `json:"beforeMetric"`
	AfterHops    int    `json:"afterHops"`
	AfterMetric  uint32 `json:"afterMetric"`
	HopReduction int    `json:"hopReduction"`
	MetricReduction uint32 `json:"metricReduction"`
}

// RedundancyGain represents a device that would gain redundancy
type RedundancyGain struct {
	DevicePK   string `json:"devicePK"`
	DeviceCode string `json:"deviceCode"`
	OldDegree  int    `json:"oldDegree"`
	NewDegree  int    `json:"newDegree"`
	WasLeaf    bool   `json:"wasLeaf"` // Was a single point of failure
}

// GetSimulateLinkAddition simulates adding a link and shows the benefits
func GetSimulateLinkAddition(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	sourcePK := r.URL.Query().Get("sourcePK")
	targetPK := r.URL.Query().Get("targetPK")
	metricStr := r.URL.Query().Get("metric")

	if sourcePK == "" || targetPK == "" {
		writeJSON(w, SimulateLinkAdditionResponse{Error: "sourcePK and targetPK parameters are required"})
		return
	}

	if sourcePK == targetPK {
		writeJSON(w, SimulateLinkAdditionResponse{Error: "sourcePK and targetPK must be different"})
		return
	}

	metric := uint32(1000) // Default 1ms metric
	if metricStr != "" {
		if parsed, err := strconv.ParseUint(metricStr, 10, 32); err == nil {
			metric = uint32(parsed)
		}
	}

	start := time.Now()

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := SimulateLinkAdditionResponse{
		SourcePK:        sourcePK,
		TargetPK:        targetPK,
		Metric:          metric,
		ImprovedPaths:   []ImprovedPath{},
		RedundancyGains: []RedundancyGain{},
	}

	// Get device codes and current degrees
	codesCypher := `
		MATCH (s:Device {pk: $source_pk})
		MATCH (t:Device {pk: $target_pk})
		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sn:Device)
		WHERE sn.isis_system_id IS NOT NULL
		WITH s, t, count(DISTINCT sn) AS sourceDegree
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tn:Device)
		WHERE tn.isis_system_id IS NOT NULL
		RETURN s.code AS source_code, t.code AS target_code,
		       sourceDegree, count(DISTINCT tn) AS targetDegree
	`
	codesResult, err := session.Run(ctx, codesCypher, map[string]any{
		"source_pk": sourcePK,
		"target_pk": targetPK,
	})
	if err != nil {
		log.Printf("Simulate link addition codes query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}

	var sourceDegree, targetDegree int
	if codesRecord, err := codesResult.Single(ctx); err == nil {
		sourceCode, _ := codesRecord.Get("source_code")
		targetCode, _ := codesRecord.Get("target_code")
		srcDeg, _ := codesRecord.Get("sourceDegree")
		tgtDeg, _ := codesRecord.Get("targetDegree")
		response.SourceCode = asString(sourceCode)
		response.TargetCode = asString(targetCode)
		sourceDegree = int(asInt64(srcDeg))
		targetDegree = int(asInt64(tgtDeg))
	}

	// Check if link already exists
	existsCypher := `
		MATCH (s:Device {pk: $source_pk})-[r:ISIS_ADJACENT]-(t:Device {pk: $target_pk})
		RETURN count(r) > 0 AS exists
	`
	existsResult, err := session.Run(ctx, existsCypher, map[string]any{
		"source_pk": sourcePK,
		"target_pk": targetPK,
	})
	if err == nil {
		if existsRecord, err := existsResult.Single(ctx); err == nil {
			exists, _ := existsRecord.Get("exists")
			if asBool(exists) {
				response.Error = "Link already exists between these devices"
				writeJSON(w, response)
				return
			}
		}
	}

	// Calculate redundancy gains
	// A device gains redundancy if it was a leaf (degree 1) and the new link increases its degree
	if sourceDegree == 1 {
		response.RedundancyGains = append(response.RedundancyGains, RedundancyGain{
			DevicePK:   sourcePK,
			DeviceCode: response.SourceCode,
			OldDegree:  sourceDegree,
			NewDegree:  sourceDegree + 1,
			WasLeaf:    true,
		})
	}
	if targetDegree == 1 {
		response.RedundancyGains = append(response.RedundancyGains, RedundancyGain{
			DevicePK:   targetPK,
			DeviceCode: response.TargetCode,
			OldDegree:  targetDegree,
			NewDegree:  targetDegree + 1,
			WasLeaf:    true,
		})
	}
	response.RedundancyCount = len(response.RedundancyGains)

	// Find paths that would be improved by the new link
	// We use a simpler approach: check current path between source and target,
	// and also check paths from their immediate neighbors
	improvedCypher := `
		// Get the source and target devices
		MATCH (src:Device {pk: $source_pk}), (tgt:Device {pk: $target_pk})

		// Get immediate neighbors of source (1 hop)
		OPTIONAL MATCH (src)-[:ISIS_ADJACENT]-(srcNeighbor:Device)
		WHERE srcNeighbor.isis_system_id IS NOT NULL AND srcNeighbor.pk <> tgt.pk
		WITH src, tgt, collect(DISTINCT srcNeighbor) AS srcNeighbors

		// Get immediate neighbors of target (1 hop)
		OPTIONAL MATCH (tgt)-[:ISIS_ADJACENT]-(tgtNeighbor:Device)
		WHERE tgtNeighbor.isis_system_id IS NOT NULL AND tgtNeighbor.pk <> src.pk
		WITH src, tgt, srcNeighbors, collect(DISTINCT tgtNeighbor) AS tgtNeighbors

		// Build device pairs to check: (source neighbors) -> (target neighbors)
		// Include src->tgt direct path check
		WITH src, tgt, srcNeighbors, tgtNeighbors,
		     [src] + srcNeighbors AS sourceSide,
		     [tgt] + tgtNeighbors AS targetSide

		UNWIND sourceSide AS from
		UNWIND targetSide AS to
		WITH src, tgt, from, to
		WHERE from.pk <> to.pk

		// Get current shortest path (OPTIONAL to handle disconnected graphs)
		OPTIONAL MATCH currentPath = shortestPath((from)-[:ISIS_ADJACENT*..10]-(to))
		WITH from, to, src, tgt, currentPath,
		     CASE WHEN currentPath IS NOT NULL THEN length(currentPath) ELSE 999 END AS currentHops,
		     CASE WHEN currentPath IS NOT NULL
		          THEN reduce(total = 0, r IN relationships(currentPath) | total + coalesce(r.metric, 0))
		          ELSE 999999 END AS currentMetric
		WHERE currentPath IS NOT NULL AND length(currentPath) > 2

		// Calculate path via new link: from -> src -> [new link] -> tgt -> to
		// Handle shortestPath carefully to avoid same start/end node error
		OPTIONAL MATCH p1 = shortestPath((from)-[:ISIS_ADJACENT*..10]-(src))
		WHERE from.pk <> src.pk
		OPTIONAL MATCH p2 = shortestPath((tgt)-[:ISIS_ADJACENT*..10]-(to))
		WHERE to.pk <> tgt.pk
		WITH from, to, src, tgt, currentHops, currentMetric, p1, p2,
		     from.pk = src.pk AS fromIsSrc,
		     to.pk = tgt.pk AS toIsTgt
		WITH from, to,
		     currentHops, currentMetric,
		     CASE WHEN fromIsSrc AND toIsTgt THEN 1
		          WHEN fromIsSrc AND p2 IS NOT NULL THEN 1 + length(p2)
		          WHEN toIsTgt AND p1 IS NOT NULL THEN length(p1) + 1
		          WHEN p1 IS NOT NULL AND p2 IS NOT NULL THEN length(p1) + 1 + length(p2)
		          ELSE 999 END AS viaNewLinkHops,
		     CASE WHEN fromIsSrc AND toIsTgt THEN $metric
		          WHEN fromIsSrc AND p2 IS NOT NULL
		               THEN $metric + reduce(t = 0, r IN relationships(p2) | t + coalesce(r.metric, 0))
		          WHEN toIsTgt AND p1 IS NOT NULL
		               THEN reduce(t = 0, r IN relationships(p1) | t + coalesce(r.metric, 0)) + $metric
		          WHEN p1 IS NOT NULL AND p2 IS NOT NULL
		               THEN reduce(t = 0, r IN relationships(p1) | t + coalesce(r.metric, 0)) + $metric +
		                    reduce(t = 0, r IN relationships(p2) | t + coalesce(r.metric, 0))
		          ELSE 999999 END AS viaNewLinkMetric

		// Only return if the new link provides improvement
		WHERE viaNewLinkHops < currentHops
		RETURN from.pk AS from_pk,
		       from.code AS from_code,
		       to.pk AS to_pk,
		       to.code AS to_code,
		       currentHops AS before_hops,
		       currentMetric AS before_metric,
		       viaNewLinkHops AS after_hops,
		       viaNewLinkMetric AS after_metric
		ORDER BY (currentHops - viaNewLinkHops) DESC
		LIMIT 15
	`

	improvedResult, err := session.Run(ctx, improvedCypher, map[string]any{
		"source_pk": sourcePK,
		"target_pk": targetPK,
		"metric":    int64(metric),
	})
	if err != nil {
		log.Printf("Simulate link addition improved paths query error: %v", err)
		response.Error = "failed to query improved paths: " + err.Error()
	} else {
		improvedRecords, err := improvedResult.Collect(ctx)
		if err != nil {
			log.Printf("Simulate link addition improved paths collect error: %v", err)
			response.Error = "failed to query improved paths: " + err.Error()
		} else {
			for _, record := range improvedRecords {
				fromPK, _ := record.Get("from_pk")
				fromCode, _ := record.Get("from_code")
				toPK, _ := record.Get("to_pk")
				toCode, _ := record.Get("to_code")
				beforeHops, _ := record.Get("before_hops")
				beforeMetric, _ := record.Get("before_metric")
				afterHops, _ := record.Get("after_hops")
				afterMetric, _ := record.Get("after_metric")

				bHops := int(asInt64(beforeHops))
				aHops := int(asInt64(afterHops))
				bMetric := uint32(asInt64(beforeMetric))
				aMetric := uint32(asInt64(afterMetric))

				response.ImprovedPaths = append(response.ImprovedPaths, ImprovedPath{
					FromPK:          asString(fromPK),
					FromCode:        asString(fromCode),
					ToPK:            asString(toPK),
					ToCode:          asString(toCode),
					BeforeHops:      bHops,
					BeforeMetric:    bMetric,
					AfterHops:       aHops,
					AfterMetric:     aMetric,
					HopReduction:    bHops - aHops,
					MetricReduction: bMetric - aMetric,
				})
			}
		}
	}
	response.ImprovedPathCount = len(response.ImprovedPaths)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("Simulate link addition: %s -> %s (metric=%d), improvedPaths=%d, redundancyGains=%d in %v",
		response.SourceCode, response.TargetCode, metric, response.ImprovedPathCount, response.RedundancyCount, duration)

	writeJSON(w, response)
}

// WhatIfRemovalRequest is the request body for unified what-if removal analysis
type WhatIfRemovalRequest struct {
	Devices []string `json:"devices"` // Device PKs
	Links   []string `json:"links"`   // Link PKs
}

// WhatIfRemovalResponse is the response for unified what-if removal analysis
type WhatIfRemovalResponse struct {
	Items              []WhatIfRemovalItem   `json:"items"`
	TotalAffectedPaths int                   `json:"totalAffectedPaths"`
	TotalDisconnected  int                   `json:"totalDisconnected"`
	AffectedPaths      []WhatIfAffectedPath  `json:"affectedPaths,omitempty"`
	DisconnectedList   []string              `json:"disconnectedList,omitempty"`
	Error              string                `json:"error,omitempty"`
}

// WhatIfRemovalItem represents impact of a single device or link removal
type WhatIfRemovalItem struct {
	Type                string               `json:"type"` // "device" or "link"
	PK                  string               `json:"pk"`   // Device PK or Link PK
	Code                string               `json:"code"` // Display name
	AffectedPaths       []WhatIfAffectedPath `json:"affectedPaths"`
	AffectedPathCount   int                  `json:"affectedPathCount"`
	DisconnectedDevices []string             `json:"disconnectedDevices"`
	DisconnectedCount   int                  `json:"disconnectedCount"`
	CausesPartition     bool                 `json:"causesPartition"`
}

// WhatIfAffectedPath represents a path affected by removal
type WhatIfAffectedPath struct {
	Source       string `json:"source"`
	Target       string `json:"target"`
	SourceMetro  string `json:"sourceMetro,omitempty"`
	TargetMetro  string `json:"targetMetro,omitempty"`
	HopsBefore   int    `json:"hopsBefore"`
	MetricBefore int    `json:"metricBefore"`
	HopsAfter    int    `json:"hopsAfter"`   // -1 if disconnected
	MetricAfter  int    `json:"metricAfter"` // -1 if disconnected
	Status       string `json:"status"`      // "rerouted", "degraded", "disconnected"
}

// PostWhatIfRemoval analyzes the impact of removing devices and/or links
func PostWhatIfRemoval(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	start := time.Now()

	// Parse request body
	var req WhatIfRemovalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, WhatIfRemovalResponse{Error: "Invalid request body: " + err.Error()})
		return
	}

	if len(req.Devices) == 0 && len(req.Links) == 0 {
		writeJSON(w, WhatIfRemovalResponse{Error: "No devices or links specified"})
		return
	}

	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	response := WhatIfRemovalResponse{
		Items:            []WhatIfRemovalItem{},
		AffectedPaths:    []WhatIfAffectedPath{},
		DisconnectedList: []string{},
	}

	// Analyze each device
	for _, devicePK := range req.Devices {
		item := analyzeDeviceRemoval(ctx, session, devicePK, 10)
		response.Items = append(response.Items, item)
		response.TotalAffectedPaths += item.AffectedPathCount
		response.TotalDisconnected += item.DisconnectedCount
		response.AffectedPaths = append(response.AffectedPaths, item.AffectedPaths...)
		response.DisconnectedList = append(response.DisconnectedList, item.DisconnectedDevices...)
	}

	// Analyze each link
	for _, linkPK := range req.Links {
		item := analyzeLinkRemoval(ctx, session, linkPK, 10)
		response.Items = append(response.Items, item)
		response.TotalAffectedPaths += item.AffectedPathCount
		response.TotalDisconnected += item.DisconnectedCount
		response.AffectedPaths = append(response.AffectedPaths, item.AffectedPaths...)
		response.DisconnectedList = append(response.DisconnectedList, item.DisconnectedDevices...)
	}

	// Limit total affected paths in response
	if len(response.AffectedPaths) > 50 {
		response.AffectedPaths = response.AffectedPaths[:50]
	}

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	log.Printf("What-if removal: %d devices, %d links, totalPaths=%d, totalDisconnected=%d in %v",
		len(req.Devices), len(req.Links), response.TotalAffectedPaths, response.TotalDisconnected, duration)

	writeJSON(w, response)
}

// analyzeDeviceRemoval computes the impact of removing a single device
func analyzeDeviceRemoval(ctx context.Context, session neo4j.Session, devicePK string, pathLimit int) WhatIfRemovalItem {
	item := WhatIfRemovalItem{
		Type:                "device",
		PK:                  devicePK,
		AffectedPaths:       []WhatIfAffectedPath{},
		DisconnectedDevices: []string{},
	}

	// Get device code and neighbor info
	infoCypher := `
		MATCH (d:Device {pk: $devicePK})
		WHERE d.isis_system_id IS NOT NULL

		// Get device code
		WITH d

		// Find all neighbors
		OPTIONAL MATCH (d)-[:ISIS_ADJACENT]-(neighbor:Device)
		WHERE neighbor.isis_system_id IS NOT NULL
		WITH d, collect(DISTINCT neighbor) AS neighbors

		// Find leaf neighbors (degree 1) that would be disconnected
		UNWIND neighbors AS neighbor
		OPTIONAL MATCH (neighbor)-[:ISIS_ADJACENT]-(neighborOfNeighbor:Device)
		WHERE neighborOfNeighbor.isis_system_id IS NOT NULL
		WITH d, neighbor, count(DISTINCT neighborOfNeighbor) AS neighborDegree
		WITH d,
		     collect(CASE WHEN neighborDegree = 1 THEN neighbor.code ELSE null END) AS leafCodes

		RETURN d.code AS code,
		       [x IN leafCodes WHERE x IS NOT NULL] AS disconnectedDevices
	`

	result, err := session.Run(ctx, infoCypher, map[string]any{"devicePK": devicePK})
	if err != nil {
		log.Printf("Device removal info query error: %v", err)
		item.Code = devicePK
		return item
	}

	if result.Next(ctx) {
		record := result.Record()
		code, _ := record.Get("code")
		disconnected, _ := record.Get("disconnectedDevices")

		item.Code = asString(code)
		if arr, ok := disconnected.([]any); ok {
			for _, v := range arr {
				if s := asString(v); s != "" {
					item.DisconnectedDevices = append(item.DisconnectedDevices, s)
				}
			}
		}
	}

	item.DisconnectedCount = len(item.DisconnectedDevices)
	item.CausesPartition = item.DisconnectedCount > 0

	// Find affected paths through this device
	pathsCypher := `
		MATCH (offline:Device {pk: $devicePK})
		WHERE offline.isis_system_id IS NOT NULL

		// Find neighbors of the offline device
		MATCH (offline)-[:ISIS_ADJACENT]-(neighbor:Device)
		WHERE neighbor.isis_system_id IS NOT NULL

		WITH offline, collect(DISTINCT neighbor) AS neighbors

		// For each pair of neighbors, check if shortest path goes through offline device
		UNWIND neighbors AS n1
		UNWIND neighbors AS n2
		WITH offline, n1, n2
		WHERE n1.pk < n2.pk

		// Get current shortest path
		MATCH currentPath = shortestPath((n1)-[:ISIS_ADJACENT*]-(n2))
		WITH offline, n1, n2, currentPath,
		     length(currentPath) AS currentHops,
		     reduce(m = 0, r IN relationships(currentPath) | m + coalesce(r.metric, 10)) AS currentMetric,
		     any(node IN nodes(currentPath) WHERE node.pk = offline.pk) AS goesThruOffline

		WHERE goesThruOffline = true

		// Get metro info
		OPTIONAL MATCH (n1)-[:LOCATED_IN]->(m1:Metro)
		OPTIONAL MATCH (n2)-[:LOCATED_IN]->(m2:Metro)

		RETURN n1.pk AS sourcePK, n1.code AS sourceCode,
		       n2.pk AS targetPK, n2.code AS targetCode,
		       COALESCE(m1.code, '') AS sourceMetro,
		       COALESCE(m2.code, '') AS targetMetro,
		       currentHops, currentMetric
		LIMIT $limit
	`

	pathsResult, err := session.Run(ctx, pathsCypher, map[string]any{
		"devicePK": devicePK,
		"limit":    pathLimit * 2, // Get more for filtering
	})
	if err != nil {
		log.Printf("Device removal paths query error: %v", err)
		return item
	}

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

	for pathsResult.Next(ctx) {
		record := pathsResult.Record()
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

	// Find alternate paths avoiding the offline device
	for _, c := range candidates {
		if len(item.AffectedPaths) >= pathLimit {
			break
		}

		path := WhatIfAffectedPath{
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

		altCypher := `
			MATCH (source:Device {pk: $sourcePK}), (target:Device {pk: $targetPK})
			MATCH altPath = shortestPath((source)-[:ISIS_ADJACENT*]-(target))
			WHERE none(n IN nodes(altPath) WHERE n.pk = $devicePK)
			WITH altPath, length(altPath) AS altHops,
			     reduce(m = 0, r IN relationships(altPath) | m + coalesce(r.metric, 10)) AS altMetric
			RETURN altHops, altMetric
			LIMIT 1
		`

		altResult, err := session.Run(ctx, altCypher, map[string]any{
			"sourcePK": c.sourcePK,
			"targetPK": c.targetPK,
			"devicePK": devicePK,
		})
		if err == nil && altResult.Next(ctx) {
			record := altResult.Record()
			altHops, _ := record.Get("altHops")
			altMetric, _ := record.Get("altMetric")

			path.HopsAfter = int(asInt64(altHops))
			path.MetricAfter = int(asInt64(altMetric))

			hopIncrease := path.HopsAfter - path.HopsBefore
			metricIncrease := path.MetricAfter - path.MetricBefore
			if hopIncrease > 2 || metricIncrease > 50 {
				path.Status = "degraded"
			} else {
				path.Status = "rerouted"
			}
		}

		item.AffectedPaths = append(item.AffectedPaths, path)
	}

	item.AffectedPathCount = len(item.AffectedPaths)
	return item
}

// analyzeLinkRemoval computes the impact of removing a single link
func analyzeLinkRemoval(ctx context.Context, session neo4j.Session, linkPK string, pathLimit int) WhatIfRemovalItem {
	item := WhatIfRemovalItem{
		Type:                "link",
		PK:                  linkPK,
		AffectedPaths:       []WhatIfAffectedPath{},
		DisconnectedDevices: []string{},
	}

	// First, resolve link PK to device endpoints via ClickHouse
	linkQuery := `
		SELECT
			COALESCE(l.side_a_pk, '') as side_a_pk,
			COALESCE(l.side_z_pk, '') as side_z_pk,
			COALESCE(da.code, '') as side_a_code,
			COALESCE(dz.code, '') as side_z_code
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		WHERE l.pk = $1
	`

	var sideAPK, sideZPK, sideACode, sideZCode string
	if err := config.DB.QueryRow(ctx, linkQuery, linkPK).Scan(&sideAPK, &sideZPK, &sideACode, &sideZCode); err != nil {
		log.Printf("Link lookup error for %s: %v", linkPK, err)
		item.Code = "Link not found"
		return item
	}

	if sideAPK == "" || sideZPK == "" {
		item.Code = "Link missing endpoints"
		return item
	}

	item.Code = sideACode + " - " + sideZCode

	// Check if either endpoint is a leaf (would be disconnected)
	disconnectCypher := `
		MATCH (s:Device {pk: $sourcePK}), (t:Device {pk: $targetPK})
		WHERE s.isis_system_id IS NOT NULL AND t.isis_system_id IS NOT NULL

		// Count neighbors of each endpoint
		OPTIONAL MATCH (s)-[:ISIS_ADJACENT]-(sn:Device)
		WHERE sn.isis_system_id IS NOT NULL
		WITH s, t, count(DISTINCT sn) AS sourceDegree
		OPTIONAL MATCH (t)-[:ISIS_ADJACENT]-(tn:Device)
		WHERE tn.isis_system_id IS NOT NULL
		WITH s, t, sourceDegree, count(DISTINCT tn) AS targetDegree

		RETURN sourceDegree, targetDegree, s.code AS sourceCode, t.code AS targetCode
	`

	degResult, err := session.Run(ctx, disconnectCypher, map[string]any{
		"sourcePK": sideAPK,
		"targetPK": sideZPK,
	})
	if err != nil {
		log.Printf("Link disconnect check error: %v", err)
	} else if degResult.Next(ctx) {
		record := degResult.Record()
		sourceDegree, _ := record.Get("sourceDegree")
		targetDegree, _ := record.Get("targetDegree")
		sourceCode, _ := record.Get("sourceCode")
		targetCode, _ := record.Get("targetCode")

		if asInt64(sourceDegree) == 1 {
			item.DisconnectedDevices = append(item.DisconnectedDevices, asString(sourceCode))
		}
		if asInt64(targetDegree) == 1 {
			item.DisconnectedDevices = append(item.DisconnectedDevices, asString(targetCode))
		}
	}

	item.DisconnectedCount = len(item.DisconnectedDevices)
	item.CausesPartition = item.DisconnectedCount > 0

	// Find affected paths - paths that currently use this link
	affectedCypher := `
		MATCH (src:Device {pk: $sourcePK}), (tgt:Device {pk: $targetPK})
		WHERE src.isis_system_id IS NOT NULL AND tgt.isis_system_id IS NOT NULL

		// Get the metric of the link being removed
		OPTIONAL MATCH (src)-[linkRel:ISIS_ADJACENT]-(tgt)
		WITH src, tgt, min(linkRel.metric) AS linkMetric

		// Get immediate neighbors of source with their link metrics
		OPTIONAL MATCH (src)-[srcRel:ISIS_ADJACENT]-(srcNeighbor:Device)
		WHERE srcNeighbor.isis_system_id IS NOT NULL AND srcNeighbor.pk <> tgt.pk
		WITH src, tgt, linkMetric, collect(DISTINCT {device: srcNeighbor, metric: srcRel.metric}) AS srcNeighborsData

		// Get immediate neighbors of target with their link metrics
		OPTIONAL MATCH (tgt)-[tgtRel:ISIS_ADJACENT]-(tgtNeighbor:Device)
		WHERE tgtNeighbor.isis_system_id IS NOT NULL AND tgtNeighbor.pk <> src.pk
		WITH src, tgt, linkMetric, srcNeighborsData, collect(DISTINCT {device: tgtNeighbor, metric: tgtRel.metric}) AS tgtNeighborsData

		// For each source neighbor, check path to target neighbors via this link
		UNWIND CASE WHEN size(srcNeighborsData) > 0 THEN srcNeighborsData ELSE [null] END AS srcData
		UNWIND CASE WHEN size(tgtNeighborsData) > 0 THEN tgtNeighborsData ELSE [null] END AS tgtData
		WITH src, tgt, linkMetric, srcData, tgtData
		WHERE srcData IS NOT NULL AND tgtData IS NOT NULL
		  AND srcData.device.pk <> tgtData.device.pk

		WITH srcData.device AS fromDevice, tgtData.device AS toDevice, src, tgt,
		     3 AS beforeHops,
		     coalesce(srcData.metric, 0) + coalesce(linkMetric, 0) + coalesce(tgtData.metric, 0) AS beforeMetric

		// Get metro info
		OPTIONAL MATCH (fromDevice)-[:LOCATED_IN]->(fm:Metro)
		OPTIONAL MATCH (toDevice)-[:LOCATED_IN]->(tm:Metro)

		RETURN fromDevice.pk AS fromPK, fromDevice.code AS fromCode,
		       toDevice.pk AS toPK, toDevice.code AS toCode,
		       COALESCE(fm.code, '') AS fromMetro,
		       COALESCE(tm.code, '') AS toMetro,
		       beforeHops, beforeMetric,
		       src.pk AS srcPK, tgt.pk AS tgtPK
		LIMIT $limit
	`

	affectedResult, err := session.Run(ctx, affectedCypher, map[string]any{
		"sourcePK": sideAPK,
		"targetPK": sideZPK,
		"limit":    pathLimit * 2,
	})
	if err != nil {
		log.Printf("Link affected paths query error: %v", err)
		return item
	}

	type pathCandidate struct {
		fromPK       string
		fromCode     string
		toPK         string
		toCode       string
		fromMetro    string
		toMetro      string
		beforeHops   int
		beforeMetric int
		srcPK        string
		tgtPK        string
	}
	candidates := []pathCandidate{}

	for affectedResult.Next(ctx) {
		record := affectedResult.Record()
		fromPK, _ := record.Get("fromPK")
		fromCode, _ := record.Get("fromCode")
		toPK, _ := record.Get("toPK")
		toCode, _ := record.Get("toCode")
		fromMetro, _ := record.Get("fromMetro")
		toMetro, _ := record.Get("toMetro")
		beforeHops, _ := record.Get("beforeHops")
		beforeMetric, _ := record.Get("beforeMetric")
		srcPK, _ := record.Get("srcPK")
		tgtPK, _ := record.Get("tgtPK")

		candidates = append(candidates, pathCandidate{
			fromPK:       asString(fromPK),
			fromCode:     asString(fromCode),
			toPK:         asString(toPK),
			toCode:       asString(toCode),
			fromMetro:    asString(fromMetro),
			toMetro:      asString(toMetro),
			beforeHops:   int(asInt64(beforeHops)),
			beforeMetric: int(asInt64(beforeMetric)),
			srcPK:        asString(srcPK),
			tgtPK:        asString(tgtPK),
		})
	}

	// Find alternate paths avoiding this link
	for _, c := range candidates {
		if len(item.AffectedPaths) >= pathLimit {
			break
		}

		path := WhatIfAffectedPath{
			Source:       c.fromCode,
			Target:       c.toCode,
			SourceMetro:  c.fromMetro,
			TargetMetro:  c.toMetro,
			HopsBefore:   c.beforeHops,
			MetricBefore: c.beforeMetric,
			HopsAfter:    -1,
			MetricAfter:  -1,
			Status:       "disconnected",
		}

		// Find alternate path not using this specific link
		altCypher := `
			MATCH (from:Device {pk: $fromPK}), (to:Device {pk: $toPK})
			OPTIONAL MATCH altPath = shortestPath((from)-[:ISIS_ADJACENT*]-(to))
			WHERE NONE(r IN relationships(altPath) WHERE
			      (startNode(r).pk = $srcPK AND endNode(r).pk = $tgtPK) OR
			      (startNode(r).pk = $tgtPK AND endNode(r).pk = $srcPK))
			WITH altPath,
			     CASE WHEN altPath IS NOT NULL THEN length(altPath) ELSE 0 END AS altHops,
			     CASE WHEN altPath IS NOT NULL
			          THEN reduce(m = 0, r IN relationships(altPath) | m + coalesce(r.metric, 10))
			          ELSE 0 END AS altMetric
			WHERE altPath IS NOT NULL
			RETURN altHops, altMetric
			LIMIT 1
		`

		altResult, err := session.Run(ctx, altCypher, map[string]any{
			"fromPK": c.fromPK,
			"toPK":   c.toPK,
			"srcPK":  c.srcPK,
			"tgtPK":  c.tgtPK,
		})
		if err == nil && altResult.Next(ctx) {
			record := altResult.Record()
			altHops, _ := record.Get("altHops")
			altMetric, _ := record.Get("altMetric")

			path.HopsAfter = int(asInt64(altHops))
			path.MetricAfter = int(asInt64(altMetric))

			// Only include if the path through the link was actually preferred
			if path.MetricBefore < path.MetricAfter || path.HopsAfter == 0 {
				hopIncrease := path.HopsAfter - path.HopsBefore
				metricIncrease := path.MetricAfter - path.MetricBefore
				if path.HopsAfter == 0 {
					path.Status = "disconnected"
					path.HopsAfter = -1
					path.MetricAfter = -1
				} else if hopIncrease > 2 || metricIncrease > 50 {
					path.Status = "degraded"
				} else {
					path.Status = "rerouted"
				}
				item.AffectedPaths = append(item.AffectedPaths, path)
			}
		} else {
			// No alternate path found
			item.AffectedPaths = append(item.AffectedPaths, path)
		}
	}

	item.AffectedPathCount = len(item.AffectedPaths)
	return item
}
