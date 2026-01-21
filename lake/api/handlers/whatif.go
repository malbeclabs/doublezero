package handlers

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
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
