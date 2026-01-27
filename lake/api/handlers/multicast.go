package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
)

type MulticastGroupListItem struct {
	PK              string `json:"pk"`
	Code            string `json:"code"`
	MulticastIP     string `json:"multicast_ip"`
	MaxBandwidth    uint64 `json:"max_bandwidth"`
	Status          string `json:"status"`
	PublisherCount  uint32 `json:"publisher_count"`
	SubscriberCount uint32 `json:"subscriber_count"`
}

func GetMulticastGroups(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	start := time.Now()

	query := `
		SELECT
			pk,
			COALESCE(code, '') as code,
			COALESCE(multicast_ip, '') as multicast_ip,
			COALESCE(max_bandwidth, 0) as max_bandwidth,
			COALESCE(status, '') as status,
			COALESCE(publisher_count, 0) as publisher_count,
			COALESCE(subscriber_count, 0) as subscriber_count
		FROM dz_multicast_groups_current
		WHERE status = 'activated'
		ORDER BY code
	`

	rows, err := config.DB.Query(ctx, query)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("MulticastGroups query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var groups []MulticastGroupListItem
	for rows.Next() {
		var g MulticastGroupListItem
		if err := rows.Scan(
			&g.PK,
			&g.Code,
			&g.MulticastIP,
			&g.MaxBandwidth,
			&g.Status,
			&g.PublisherCount,
			&g.SubscriberCount,
		); err != nil {
			log.Printf("MulticastGroups scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		log.Printf("MulticastGroups rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if groups == nil {
		groups = []MulticastGroupListItem{}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(groups); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

type MulticastMember struct {
	UserPK      string `json:"user_pk"`
	Mode        string `json:"mode"` // "P", "S", or "P+S"
	DevicePK    string `json:"device_pk"`
	DeviceCode  string `json:"device_code"`
	MetroPK     string `json:"metro_pk"`
	MetroCode   string `json:"metro_code"`
	MetroName   string `json:"metro_name"`
	ClientIP    string `json:"client_ip"`
	DZIP        string `json:"dz_ip"`
	Status      string `json:"status"`
	OwnerPubkey string `json:"owner_pubkey"`
}

type MulticastGroupDetail struct {
	PK              string            `json:"pk"`
	Code            string            `json:"code"`
	MulticastIP     string            `json:"multicast_ip"`
	MaxBandwidth    uint64            `json:"max_bandwidth"`
	Status          string            `json:"status"`
	PublisherCount  uint32            `json:"publisher_count"`
	SubscriberCount uint32            `json:"subscriber_count"`
	Members         []MulticastMember `json:"members"`
}

func GetMulticastGroup(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	code := chi.URLParam(r, "code")
	if code == "" {
		http.Error(w, "missing multicast group code", http.StatusBadRequest)
		return
	}

	start := time.Now()

	// First get the group details
	groupQuery := `
		SELECT
			pk,
			COALESCE(code, '') as code,
			COALESCE(multicast_ip, '') as multicast_ip,
			COALESCE(max_bandwidth, 0) as max_bandwidth,
			COALESCE(status, '') as status,
			COALESCE(publisher_count, 0) as publisher_count,
			COALESCE(subscriber_count, 0) as subscriber_count
		FROM dz_multicast_groups_current
		WHERE code = ?
	`

	var group MulticastGroupDetail
	err := config.DB.QueryRow(ctx, groupQuery, code).Scan(
		&group.PK,
		&group.Code,
		&group.MulticastIP,
		&group.MaxBandwidth,
		&group.Status,
		&group.PublisherCount,
		&group.SubscriberCount,
	)
	if err != nil {
		log.Printf("MulticastGroup query error: %v", err)
		http.Error(w, "multicast group not found", http.StatusNotFound)
		return
	}

	// Now get all members (users who publish or subscribe to this group)
	// Users have publishers and subscribers columns as JSON arrays of group PKs
	membersQuery := `
		SELECT
			u.pk as user_pk,
			CASE
				WHEN has(JSONExtract(u.publishers, 'Array(String)'), ?) AND has(JSONExtract(u.subscribers, 'Array(String)'), ?) THEN 'P+S'
				WHEN has(JSONExtract(u.publishers, 'Array(String)'), ?) THEN 'P'
				ELSE 'S'
			END as mode,
			COALESCE(u.device_pk, '') as device_pk,
			COALESCE(d.code, '') as device_code,
			COALESCE(d.metro_pk, '') as metro_pk,
			COALESCE(m.code, '') as metro_code,
			COALESCE(m.name, '') as metro_name,
			COALESCE(u.client_ip, '') as client_ip,
			COALESCE(u.dz_ip, '') as dz_ip,
			u.status,
			COALESCE(u.owner_pubkey, '') as owner_pubkey
		FROM dz_users_current u
		LEFT JOIN dz_devices_current d ON u.device_pk = d.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		WHERE u.status = 'activated'
			AND u.kind = 'multicast'
			AND (
				has(JSONExtract(u.publishers, 'Array(String)'), ?)
				OR has(JSONExtract(u.subscribers, 'Array(String)'), ?)
			)
		ORDER BY mode DESC, d.code
	`

	rows, err := config.DB.Query(ctx, membersQuery, group.PK, group.PK, group.PK, group.PK, group.PK)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("MulticastGroup members query error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var members []MulticastMember
	for rows.Next() {
		var m MulticastMember
		if err := rows.Scan(
			&m.UserPK,
			&m.Mode,
			&m.DevicePK,
			&m.DeviceCode,
			&m.MetroPK,
			&m.MetroCode,
			&m.MetroName,
			&m.ClientIP,
			&m.DZIP,
			&m.Status,
			&m.OwnerPubkey,
		); err != nil {
			log.Printf("MulticastGroup members scan error: %v", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		members = append(members, m)
	}

	if err := rows.Err(); err != nil {
		log.Printf("MulticastGroup members rows error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return empty array instead of null
	if members == nil {
		members = []MulticastMember{}
	}
	group.Members = members

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(group); err != nil {
		log.Printf("JSON encoding error: %v", err)
	}
}

// MulticastTreeHop represents a single hop in a multicast tree path
type MulticastTreeHop struct {
	DevicePK   string `json:"devicePK"`
	DeviceCode string `json:"deviceCode"`
	DeviceType string `json:"deviceType"`
	EdgeMetric int    `json:"edgeMetric,omitempty"` // metric to reach this hop from previous
}

// MulticastTreePath represents a path from publisher to subscriber
type MulticastTreePath struct {
	PublisherDevicePK   string             `json:"publisherDevicePK"`
	PublisherDeviceCode string             `json:"publisherDeviceCode"`
	SubscriberDevicePK  string             `json:"subscriberDevicePK"`
	SubscriberDeviceCode string            `json:"subscriberDeviceCode"`
	Path                []MulticastTreeHop `json:"path"`
	TotalMetric         int                `json:"totalMetric"`
	HopCount            int                `json:"hopCount"`
}

// MulticastTreeResponse is the response for multicast tree paths endpoint
type MulticastTreeResponse struct {
	GroupCode       string              `json:"groupCode"`
	GroupPK         string              `json:"groupPK"`
	PublisherCount  int                 `json:"publisherCount"`
	SubscriberCount int                 `json:"subscriberCount"`
	Paths           []MulticastTreePath `json:"paths"`
	Error           string              `json:"error,omitempty"`
}

// GetMulticastTreePaths computes paths from all publishers to all subscribers in a multicast group
func GetMulticastTreePaths(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	code := chi.URLParam(r, "code")
	if code == "" {
		writeJSON(w, MulticastTreeResponse{Error: "missing multicast group code"})
		return
	}

	start := time.Now()
	response := MulticastTreeResponse{
		GroupCode: code,
		Paths:     []MulticastTreePath{},
	}

	// First get group info and members from ClickHouse
	groupQuery := `
		SELECT pk FROM dz_multicast_groups_current WHERE code = ?
	`
	err := config.DB.QueryRow(ctx, groupQuery, code).Scan(&response.GroupPK)
	if err != nil {
		log.Printf("MulticastTreePaths group query error: %v", err)
		response.Error = "multicast group not found"
		writeJSON(w, response)
		return
	}

	// Get publishers and subscribers
	membersQuery := `
		SELECT
			CASE
				WHEN has(JSONExtract(u.publishers, 'Array(String)'), ?) AND has(JSONExtract(u.subscribers, 'Array(String)'), ?) THEN 'P+S'
				WHEN has(JSONExtract(u.publishers, 'Array(String)'), ?) THEN 'P'
				ELSE 'S'
			END as mode,
			COALESCE(u.device_pk, '') as device_pk,
			COALESCE(d.code, '') as device_code
		FROM dz_users_current u
		LEFT JOIN dz_devices_current d ON u.device_pk = d.pk
		WHERE u.status = 'activated'
			AND u.kind = 'multicast'
			AND (
				has(JSONExtract(u.publishers, 'Array(String)'), ?)
				OR has(JSONExtract(u.subscribers, 'Array(String)'), ?)
			)
	`

	rows, err := config.DB.Query(ctx, membersQuery, response.GroupPK, response.GroupPK, response.GroupPK, response.GroupPK, response.GroupPK)
	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, err)

	if err != nil {
		log.Printf("MulticastTreePaths members query error: %v", err)
		response.Error = err.Error()
		writeJSON(w, response)
		return
	}
	defer rows.Close()

	type deviceInfo struct {
		PK   string
		Code string
	}
	var publishers, subscribers []deviceInfo
	publisherSet := make(map[string]bool)
	subscriberSet := make(map[string]bool)

	for rows.Next() {
		var mode, devicePK, deviceCode string
		if err := rows.Scan(&mode, &devicePK, &deviceCode); err != nil {
			log.Printf("MulticastTreePaths members scan error: %v", err)
			continue
		}
		if devicePK == "" {
			continue // Skip members without device assignment
		}

		// Publishers: P or P+S
		if (mode == "P" || mode == "P+S") && !publisherSet[devicePK] {
			publishers = append(publishers, deviceInfo{PK: devicePK, Code: deviceCode})
			publisherSet[devicePK] = true
		}
		// Subscribers: S or P+S
		if (mode == "S" || mode == "P+S") && !subscriberSet[devicePK] {
			subscribers = append(subscribers, deviceInfo{PK: devicePK, Code: deviceCode})
			subscriberSet[devicePK] = true
		}
	}

	response.PublisherCount = len(publishers)
	response.SubscriberCount = len(subscribers)

	if len(publishers) == 0 || len(subscribers) == 0 {
		response.Error = "no publishers or subscribers found with device assignments"
		writeJSON(w, response)
		return
	}

	// Find paths from each publisher to each subscriber using Neo4j
	type pathResult struct {
		path MulticastTreePath
		err  error
	}

	var wg sync.WaitGroup
	resultChan := make(chan pathResult, len(publishers)*len(subscribers))
	sem := make(chan struct{}, 10) // Limit concurrent queries

	for _, pub := range publishers {
		for _, sub := range subscribers {
			if pub.PK == sub.PK {
				continue // Skip self-paths
			}
			wg.Add(1)
			go func(pubPK, pubCode, subPK, subCode string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				queryCtx, queryCancel := context.WithTimeout(ctx, 5*time.Second)
				defer queryCancel()

				session := config.Neo4jSession(queryCtx)
				defer session.Close(queryCtx)

				// Use Dijkstra to find lowest latency path from publisher to subscriber
				cypher := `
					MATCH (a:Device {pk: $from_pk}), (b:Device {pk: $to_pk})
					CALL apoc.algo.dijkstra(a, b, 'ISIS_ADJACENT>', 'metric') YIELD path, weight
					WITH path, toInteger(weight) AS totalMetric
					RETURN [n IN nodes(path) | {
						pk: n.pk,
						code: n.code,
						device_type: n.device_type
					}] AS devices,
					[r IN relationships(path) | r.metric] AS edgeMetrics,
					totalMetric
					LIMIT 1
				`

				result, err := session.Run(queryCtx, cypher, map[string]any{
					"from_pk": pubPK,
					"to_pk":   subPK,
				})
				if err != nil {
					resultChan <- pathResult{err: err}
					return
				}

				record, err := result.Single(queryCtx)
				if err != nil {
					// No path found - not an error, just skip
					return
				}

				// Parse the path
				devicesVal, _ := record.Get("devices")
				edgeMetricsVal, _ := record.Get("edgeMetrics")
				totalMetric, _ := record.Get("totalMetric")

				var hops []MulticastTreeHop
				if deviceList, ok := devicesVal.([]any); ok {
					var metrics []int
					if metricList, ok := edgeMetricsVal.([]any); ok {
						for _, m := range metricList {
							if v, ok := m.(int64); ok {
								metrics = append(metrics, int(v))
							}
						}
					}

					for i, d := range deviceList {
						if dm, ok := d.(map[string]any); ok {
							hop := MulticastTreeHop{
								DevicePK:   asString(dm["pk"]),
								DeviceCode: asString(dm["code"]),
								DeviceType: asString(dm["device_type"]),
							}
							// Edge metric is from previous hop to this hop
							if i > 0 && i-1 < len(metrics) {
								hop.EdgeMetric = metrics[i-1]
							}
							hops = append(hops, hop)
						}
					}
				}

				if len(hops) > 0 {
					treePath := MulticastTreePath{
						PublisherDevicePK:    pubPK,
						PublisherDeviceCode:  pubCode,
						SubscriberDevicePK:   subPK,
						SubscriberDeviceCode: subCode,
						Path:                 hops,
						HopCount:             len(hops) - 1,
					}
					if tm, ok := totalMetric.(int64); ok {
						treePath.TotalMetric = int(tm)
					}
					resultChan <- pathResult{path: treePath}
				}
			}(pub.PK, pub.Code, sub.PK, sub.Code)
		}
	}

	// Wait for all goroutines and close channel
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// Collect results
	for result := range resultChan {
		if result.err != nil {
			log.Printf("MulticastTreePaths path query error: %v", result.err)
			continue
		}
		response.Paths = append(response.Paths, result.path)
	}

	log.Printf("MulticastTreePaths: %d paths found in %v", len(response.Paths), time.Since(start))
	writeJSON(w, response)
}
