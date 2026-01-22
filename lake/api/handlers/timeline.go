package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"golang.org/x/sync/errgroup"
)

// TimelineEvent represents a single event in the timeline
type TimelineEvent struct {
	ID          string `json:"id"`
	EventType   string `json:"event_type"`
	Timestamp   string `json:"timestamp"`
	Category    string `json:"category"`
	Severity    string `json:"severity"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	EntityType  string `json:"entity_type"`
	EntityPK    string `json:"entity_pk"`
	EntityCode  string `json:"entity_code"`
	Details     any    `json:"details,omitempty"`
}

// EntityChangeDetails contains details for entity change events
type EntityChangeDetails struct {
	ChangeType string        `json:"change_type"` // "created", "updated", "deleted"
	Changes    []FieldChange `json:"changes,omitempty"`
	Entity     any           `json:"entity,omitempty"` // Full entity data
}

// FieldChange represents a single field that changed
type FieldChange struct {
	Field    string `json:"field"`
	OldValue any    `json:"old_value,omitempty"`
	NewValue any    `json:"new_value,omitempty"`
}

// DeviceEntity represents a device's current state
type DeviceEntity struct {
	PK            string `json:"pk"`
	Code          string `json:"code"`
	Status        string `json:"status"`
	DeviceType    string `json:"device_type"`
	PublicIP      string `json:"public_ip"`
	ContributorPK string `json:"contributor_pk"`
	MetroPK       string `json:"metro_pk"`
	MaxUsers      int32  `json:"max_users"`
	// Joined fields
	ContributorCode string `json:"contributor_code,omitempty"`
	MetroCode       string `json:"metro_code,omitempty"`
}

// LinkEntity represents a link's current state
type LinkEntity struct {
	PK                 string `json:"pk"`
	Code               string `json:"code"`
	Status             string `json:"status"`
	LinkType           string `json:"link_type"`
	TunnelNet          string `json:"tunnel_net"`
	ContributorPK      string `json:"contributor_pk"`
	SideAPK            string `json:"side_a_pk"`
	SideZPK            string `json:"side_z_pk"`
	SideAIfaceName     string `json:"side_a_iface_name"`
	SideZIfaceName     string `json:"side_z_iface_name"`
	CommittedRttNs     int64  `json:"committed_rtt_ns"`
	CommittedJitterNs  int64  `json:"committed_jitter_ns"`
	BandwidthBps       int64  `json:"bandwidth_bps"`
	ISISDelayOverride  int64  `json:"isis_delay_override_ns"`
	// Joined fields
	ContributorCode string `json:"contributor_code,omitempty"`
	SideACode       string `json:"side_a_code,omitempty"`
	SideZCode       string `json:"side_z_code,omitempty"`
	SideAMetroCode  string `json:"side_a_metro_code,omitempty"`
	SideZMetroCode  string `json:"side_z_metro_code,omitempty"`
	SideAMetroPK    string `json:"side_a_metro_pk,omitempty"`
	SideZMetroPK    string `json:"side_z_metro_pk,omitempty"`
}

// MetroEntity represents a metro's current state
type MetroEntity struct {
	PK        string  `json:"pk"`
	Code      string  `json:"code"`
	Name      string  `json:"name"`
	Longitude float64 `json:"longitude"`
	Latitude  float64 `json:"latitude"`
}

// ContributorEntity represents a contributor's current state
type ContributorEntity struct {
	PK   string `json:"pk"`
	Code string `json:"code"`
	Name string `json:"name"`
}

// UserEntity represents a user's current state
type UserEntity struct {
	PK          string `json:"pk"`
	OwnerPubkey string `json:"owner_pubkey"`
	Status      string `json:"status"`
	Kind        string `json:"kind"`
	ClientIP    string `json:"client_ip"`
	DZIP        string `json:"dz_ip"`
	DevicePK    string `json:"device_pk"`
	TunnelID    int32  `json:"tunnel_id"`
	// Joined fields
	DeviceCode string `json:"device_code,omitempty"`
	MetroCode  string `json:"metro_code,omitempty"`
}

// PacketLossEventDetails contains details for packet loss events
type PacketLossEventDetails struct {
	LinkPK          string  `json:"link_pk"`
	LinkCode        string  `json:"link_code"`
	LinkType        string  `json:"link_type"`
	SideAMetro      string  `json:"side_a_metro"`
	SideZMetro      string  `json:"side_z_metro"`
	PreviousLossPct float64 `json:"previous_loss_pct"`
	CurrentLossPct  float64 `json:"current_loss_pct"`
	Direction       string  `json:"direction"` // "increased" or "decreased"
}

// InterfaceEventDetails contains details for interface telemetry events
type InterfaceEventDetails struct {
	DevicePK           string `json:"device_pk"`
	DeviceCode         string `json:"device_code"`
	InterfaceName      string `json:"interface_name"`
	LinkPK             string `json:"link_pk,omitempty"`
	LinkCode           string `json:"link_code,omitempty"`
	InErrors           int64  `json:"in_errors,omitempty"`
	OutErrors          int64  `json:"out_errors,omitempty"`
	InDiscards         int64  `json:"in_discards,omitempty"`
	OutDiscards        int64  `json:"out_discards,omitempty"`
	CarrierTransitions int64  `json:"carrier_transitions,omitempty"`
	IssueType          string `json:"issue_type"` // "errors", "discards", "carrier"
}

// GroupedInterfaceDetails contains details for grouped interface events (multiple interfaces on same device)
type GroupedInterfaceDetails struct {
	DevicePK   string                  `json:"device_pk"`
	DeviceCode string                  `json:"device_code"`
	IssueType  string                  `json:"issue_type"` // "errors", "discards", "carrier"
	Interfaces []InterfaceEventDetails `json:"interfaces"`
}

// ValidatorEventDetails contains details for validator join/leave events
type ValidatorEventDetails struct {
	OwnerPubkey   string  `json:"owner_pubkey"`
	DZIP          string  `json:"dz_ip,omitempty"`
	VotePubkey    string  `json:"vote_pubkey,omitempty"`
	NodePubkey    string  `json:"node_pubkey,omitempty"`
	StakeLamports int64   `json:"stake_lamports,omitempty"`
	StakeSol      float64 `json:"stake_sol,omitempty"`
	StakeSharePct float64 `json:"stake_share_pct,omitempty"`
	UserPK        string  `json:"user_pk,omitempty"`
	DevicePK      string  `json:"device_pk,omitempty"`
	DeviceCode    string  `json:"device_code,omitempty"`
	MetroCode     string  `json:"metro_code,omitempty"`
	Kind          string  `json:"kind"` // "validator" or "gossip_only"
	Action        string  `json:"action"` // "joined" or "left"
}

// HistogramBucket represents a time bucket with event counts
type HistogramBucket struct {
	Timestamp string `json:"timestamp"`
	Count     int    `json:"count"`
}

// TimelineResponse is the API response for the timeline endpoint
type TimelineResponse struct {
	Events    []TimelineEvent   `json:"events"`
	Total     int               `json:"total"`
	Limit     int               `json:"limit"`
	Offset    int               `json:"offset"`
	TimeRange TimeRange         `json:"time_range"`
	Histogram []HistogramBucket `json:"histogram,omitempty"`
	Error     string            `json:"error,omitempty"`
}

// TimeRange represents the time range for the query
type TimeRange struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

// TimelineParams holds parsed query parameters
type TimelineParams struct {
	StartTime       time.Time
	EndTime         time.Time
	Categories      []string // "state_change" or "telemetry"
	EntityTypes     []string // "device", "link", "metro", "contributor", "validator", "gossip_node"
	Severities      []string
	Actions         []string // "added", "removed", "changed", "alerting", "resolved"
	DZFilter        string   // "on_dz", "off_dz", or "" for all
	Search          []string // Search terms to filter by (entity codes, device codes, etc.)
	Limit           int
	Offset          int
	IncludeInternal bool // Whether to include internal users (default: false)
}

// Internal user pubkeys to exclude by default
var internalUserPubkeys = []string{
	"DZfHfcCXTLwgZeCRKQ1FL1UuwAwFAZM93g86NMYpfYan",
}

// Packet loss thresholds
const (
	PacketLossWarningPct  = 0.1 // 0.1%
	PacketLossCriticalPct = 1.0 // 1%
)

// TimelineBoundsResponse contains the available date range for timeline data
type TimelineBoundsResponse struct {
	EarliestData string `json:"earliest_data"` // ISO 8601 timestamp
	LatestData   string `json:"latest_data"`   // ISO 8601 timestamp
}

// GetTimelineBounds returns the available date range for timeline data
func GetTimelineBounds(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	// Query the earliest snapshot_ts across all history tables
	query := `
		SELECT
			min(earliest) as earliest,
			max(latest) as latest
		FROM (
			SELECT min(snapshot_ts) as earliest, max(snapshot_ts) as latest FROM dim_dz_devices_history
			UNION ALL
			SELECT min(snapshot_ts), max(snapshot_ts) FROM dim_dz_links_history
			UNION ALL
			SELECT min(snapshot_ts), max(snapshot_ts) FROM dim_dz_users_history
			UNION ALL
			SELECT min(event_ts), max(event_ts) FROM fact_dz_device_link_latency
		)
	`

	var earliest, latest time.Time
	err := config.DB.QueryRow(ctx, query).Scan(&earliest, &latest)
	if err != nil {
		http.Error(w, "Failed to get timeline bounds", http.StatusInternalServerError)
		return
	}

	resp := TimelineBoundsResponse{
		EarliestData: earliest.Format(time.RFC3339),
		LatestData:   latest.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func parseTimelineParams(r *http.Request) TimelineParams {
	now := time.Now().UTC()
	endTime := now
	startTime := now.Add(-24 * time.Hour) // Default 24h

	// Check for custom start/end dates first (takes precedence over range)
	// Support both RFC3339 and datetime-local format (YYYY-MM-DDTHH:MM)
	parseTime := func(s string) (time.Time, bool) {
		if parsed, err := time.Parse(time.RFC3339, s); err == nil {
			return parsed, true
		}
		if parsed, err := time.Parse("2006-01-02T15:04", s); err == nil {
			return parsed.UTC(), true
		}
		return time.Time{}, false
	}
	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if parsed, ok := parseTime(startStr); ok {
			startTime = parsed
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if parsed, ok := parseTime(endStr); ok {
			endTime = parsed
		}
	}

	// Parse range parameter (only used if custom dates not provided)
	if r.URL.Query().Get("start") == "" && r.URL.Query().Get("end") == "" {
		if rangeStr := r.URL.Query().Get("range"); rangeStr != "" {
			switch rangeStr {
			case "1h":
				startTime = now.Add(-1 * time.Hour)
			case "6h":
				startTime = now.Add(-6 * time.Hour)
			case "12h":
				startTime = now.Add(-12 * time.Hour)
			case "24h":
				startTime = now.Add(-24 * time.Hour)
			case "3d":
				startTime = now.Add(-72 * time.Hour)
			case "7d":
				startTime = now.Add(-168 * time.Hour)
			}
		}
	}

	// Parse category filter (state_change or telemetry)
	var categories []string
	if catStr := r.URL.Query().Get("category"); catStr != "" {
		categories = strings.Split(catStr, ",")
	}

	// Parse entity type filter
	var entityTypes []string
	if etStr := r.URL.Query().Get("entity_type"); etStr != "" {
		entityTypes = strings.Split(etStr, ",")
	}

	// Parse severity filter
	var severities []string
	if sevStr := r.URL.Query().Get("severity"); sevStr != "" {
		severities = strings.Split(sevStr, ",")
	}

	// Parse pagination
	pagination := ParsePagination(r, 50)
	if pagination.Limit > 500 {
		pagination.Limit = 500 // Lower max for timeline to avoid huge result sets
	}

	// Parse include_internal filter (default: false - exclude internal users)
	includeInternal := r.URL.Query().Get("include_internal") == "true"

	// Parse action filter (added, removed, changed, alerting, resolved)
	var actions []string
	if actStr := r.URL.Query().Get("action"); actStr != "" {
		actions = strings.Split(actStr, ",")
	}

	// Parse DZ filter for Solana events (on_dz, off_dz, or empty for all)
	dzFilter := r.URL.Query().Get("dz_filter")

	// Parse search filter (comma-separated search terms)
	var search []string
	if searchStr := r.URL.Query().Get("search"); searchStr != "" {
		for _, s := range strings.Split(searchStr, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				search = append(search, strings.ToLower(s))
			}
		}
	}

	return TimelineParams{
		StartTime:       startTime,
		EndTime:         endTime,
		Categories:      categories,
		EntityTypes:     entityTypes,
		Severities:      severities,
		Actions:         actions,
		DZFilter:        dzFilter,
		Search:          search,
		Limit:           pagination.Limit,
		Offset:          pagination.Offset,
		IncludeInternal: includeInternal,
	}
}

func generateEventID(entityID string, timestamp time.Time, eventType string) string {
	h := sha256.New()
	h.Write([]byte(fmt.Sprintf("%s:%s:%s", entityID, timestamp.Format(time.RFC3339Nano), eventType)))
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// eventMatchesSearch checks if an event matches any of the search terms (OR logic)
// Search terms are already lowercased
func eventMatchesSearch(event TimelineEvent, searchTerms []string) bool {
	// Helper to check if a string contains any search term
	containsAny := func(s string) bool {
		if s == "" {
			return false
		}
		lower := strings.ToLower(s)
		for _, term := range searchTerms {
			if strings.Contains(lower, term) {
				return true
			}
		}
		return false
	}

	// Check entity_code (always present)
	if containsAny(event.EntityCode) {
		return true
	}

	// Check title
	if containsAny(event.Title) {
		return true
	}

	// Check details based on type
	switch details := event.Details.(type) {
	case EntityChangeDetails:
		if details.Entity != nil {
			// Use reflection-free approach: check known fields
			if entity, ok := details.Entity.(LinkEntity); ok {
				if containsAny(entity.Code) || containsAny(entity.SideACode) || containsAny(entity.SideZCode) ||
					containsAny(entity.SideAMetroCode) || containsAny(entity.SideZMetroCode) ||
					containsAny(entity.ContributorCode) {
					return true
				}
			}
			if entity, ok := details.Entity.(DeviceEntity); ok {
				if containsAny(entity.Code) || containsAny(entity.MetroCode) || containsAny(entity.ContributorCode) ||
					containsAny(entity.PublicIP) {
					return true
				}
			}
			if entity, ok := details.Entity.(MetroEntity); ok {
				if containsAny(entity.Code) || containsAny(entity.Name) {
					return true
				}
			}
			if entity, ok := details.Entity.(ContributorEntity); ok {
				if containsAny(entity.Code) || containsAny(entity.Name) {
					return true
				}
			}
			if entity, ok := details.Entity.(UserEntity); ok {
				if containsAny(entity.OwnerPubkey) || containsAny(entity.DeviceCode) ||
					containsAny(entity.MetroCode) || containsAny(entity.ClientIP) || containsAny(entity.DZIP) {
					return true
				}
			}
		}
	case PacketLossEventDetails:
		if containsAny(details.LinkCode) || containsAny(details.SideAMetro) || containsAny(details.SideZMetro) {
			return true
		}
	case InterfaceEventDetails:
		if containsAny(details.DeviceCode) || containsAny(details.LinkCode) || containsAny(details.InterfaceName) {
			return true
		}
	case GroupedInterfaceDetails:
		if containsAny(details.DeviceCode) {
			return true
		}
		for _, intf := range details.Interfaces {
			if containsAny(intf.LinkCode) || containsAny(intf.InterfaceName) {
				return true
			}
		}
	case ValidatorEventDetails:
		if containsAny(details.OwnerPubkey) || containsAny(details.DeviceCode) ||
			containsAny(details.MetroCode) || containsAny(details.VotePubkey) || containsAny(details.NodePubkey) {
			return true
		}
	}

	return false
}

// isDefaultTimelineRequest checks if the request matches the default cached parameters
func isDefaultTimelineRequest(r *http.Request) bool {
	q := r.URL.Query()

	// Must be 24h range (default) or no range specified
	rangeParam := q.Get("range")
	if rangeParam != "" && rangeParam != "24h" {
		return false
	}

	// Must not have custom start/end
	if q.Get("start") != "" || q.Get("end") != "" {
		return false
	}

	// Must not have category filter
	if q.Get("category") != "" {
		return false
	}

	// Entity type filter must be empty OR match the default (all except gossip_node)
	// The frontend sends "device,link,metro,contributor,user,validator" by default
	entityType := q.Get("entity_type")
	defaultEntityTypes := "device,link,metro,contributor,user,validator"
	if entityType != "" && entityType != defaultEntityTypes {
		return false
	}

	// Must not have severity filter
	if q.Get("severity") != "" {
		return false
	}

	// Must not have action filter
	if q.Get("action") != "" {
		return false
	}

	// Must be dz_filter=on_dz or no filter (defaults to on_dz)
	dzFilter := q.Get("dz_filter")
	if dzFilter != "" && dzFilter != "on_dz" {
		return false
	}

	// Must not include internal users
	if q.Get("include_internal") == "true" {
		return false
	}

	// Must not have pagination offset
	if q.Get("offset") != "" && q.Get("offset") != "0" {
		return false
	}

	// Must not have search filter
	if q.Get("search") != "" {
		return false
	}

	// Limit must be default (50) or not specified
	limit := q.Get("limit")
	if limit != "" && limit != "50" {
		return false
	}

	return true
}

// GetTimeline returns timeline events across the network
func GetTimeline(w http.ResponseWriter, r *http.Request) {
	// Check if this is a default request that can be served from cache
	if isDefaultTimelineRequest(r) && statusCache != nil {
		if cached := statusCache.GetTimeline(); cached != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Cache", "HIT")
			if err := json.NewEncoder(w).Encode(cached); err != nil {
				log.Printf("Error encoding cached timeline response: %v", err)
			}
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	start := time.Now()
	params := parseTimelineParams(r)

	g, ctx := errgroup.WithContext(ctx)
	var (
		deviceEvents      []TimelineEvent
		linkEvents        []TimelineEvent
		metroEvents       []TimelineEvent
		contributorEvents []TimelineEvent
		userEvents        []TimelineEvent
		packetLossEvents  []TimelineEvent
		interfaceEvents   []TimelineEvent
		validatorEvents   []TimelineEvent
		mu                sync.Mutex
	)

	// Check if category is requested (empty means all)
	shouldIncludeCategory := func(category string) bool {
		if len(params.Categories) == 0 {
			return true
		}
		for _, c := range params.Categories {
			if c == category {
				return true
			}
		}
		return false
	}

	// Query device changes
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryDeviceChanges(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying device changes: %v", err)
				return nil // Don't fail the whole request
			}
			mu.Lock()
			deviceEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query link changes
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryLinkChanges(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying link changes: %v", err)
				return nil
			}
			mu.Lock()
			linkEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query metro changes
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryMetroChanges(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying metro changes: %v", err)
				return nil
			}
			mu.Lock()
			metroEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query contributor changes
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryContributorChanges(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying contributor changes: %v", err)
				return nil
			}
			mu.Lock()
			contributorEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query user changes (non-validator, non-gossip users)
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryUserChanges(ctx, params.StartTime, params.EndTime, params.IncludeInternal)
			if err != nil {
				log.Printf("Error querying user changes: %v", err)
				return nil
			}
			mu.Lock()
			userEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query packet loss events
	if shouldIncludeCategory("packet_loss") {
		g.Go(func() error {
			events, err := queryPacketLossEvents(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying packet loss events: %v", err)
				return nil
			}
			mu.Lock()
			packetLossEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query interface telemetry events (carrier, errors, discards)
	if shouldIncludeCategory("interface_carrier") || shouldIncludeCategory("interface_errors") || shouldIncludeCategory("interface_discards") {
		g.Go(func() error {
			events, err := queryInterfaceEvents(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying interface events: %v", err)
				return nil
			}
			mu.Lock()
			interfaceEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query validator/gossip node events
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryValidatorEvents(ctx, params.StartTime, params.EndTime, params.IncludeInternal)
			if err != nil {
				log.Printf("Error querying validator events: %v", err)
				return nil
			}
			mu.Lock()
			validatorEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query gossip node network changes (nodes going online/offline on Solana)
	var gossipNetworkEvents []TimelineEvent
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryGossipNetworkChanges(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying gossip network changes: %v", err)
				return nil
			}
			mu.Lock()
			gossipNetworkEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query vote account changes (validators joining/leaving the network)
	var voteAccountEvents []TimelineEvent
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryVoteAccountChanges(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying vote account changes: %v", err)
				return nil
			}
			mu.Lock()
			voteAccountEvents = events
			mu.Unlock()
			return nil
		})
	}

	// Query stake changes (significant stake increases/decreases)
	var stakeChangeEvents []TimelineEvent
	if shouldIncludeCategory("state_change") {
		g.Go(func() error {
			events, err := queryStakeChanges(ctx, params.StartTime, params.EndTime)
			if err != nil {
				log.Printf("Error querying stake changes: %v", err)
				return nil
			}
			mu.Lock()
			stakeChangeEvents = events
			mu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		log.Printf("Error in timeline queries: %v", err)
	}

	// Merge all events
	allEvents := make([]TimelineEvent, 0)
	allEvents = append(allEvents, deviceEvents...)
	allEvents = append(allEvents, linkEvents...)
	allEvents = append(allEvents, metroEvents...)
	allEvents = append(allEvents, contributorEvents...)
	allEvents = append(allEvents, userEvents...)
	allEvents = append(allEvents, packetLossEvents...)
	allEvents = append(allEvents, interfaceEvents...)
	allEvents = append(allEvents, validatorEvents...)
	allEvents = append(allEvents, gossipNetworkEvents...)
	allEvents = append(allEvents, voteAccountEvents...)
	allEvents = append(allEvents, stakeChangeEvents...)

	// Group interface events by device + event type + timestamp
	allEvents = groupInterfaceEvents(allEvents)

	// Filter by entity type if specified
	if len(params.EntityTypes) > 0 {
		filtered := make([]TimelineEvent, 0)
		for _, e := range allEvents {
			for _, et := range params.EntityTypes {
				if e.EntityType == et {
					filtered = append(filtered, e)
					break
				}
			}
		}
		allEvents = filtered
	}

	// Filter by severity if specified
	if len(params.Severities) > 0 {
		filtered := make([]TimelineEvent, 0)
		for _, e := range allEvents {
			for _, s := range params.Severities {
				if e.Severity == s {
					filtered = append(filtered, e)
					break
				}
			}
		}
		allEvents = filtered
	}

	// Filter by action if specified
	// Maps action categories to event type patterns
	if len(params.Actions) > 0 {
		filtered := make([]TimelineEvent, 0)
		for _, e := range allEvents {
			for _, action := range params.Actions {
				matched := false
				switch action {
				case "added":
					// created, joined
					matched = strings.HasSuffix(e.EventType, "_created") || strings.HasSuffix(e.EventType, "_joined")
				case "removed":
					// deleted, left
					matched = strings.HasSuffix(e.EventType, "_deleted") || strings.HasSuffix(e.EventType, "_left")
				case "changed":
					// updated
					matched = strings.HasSuffix(e.EventType, "_updated")
				case "alerting":
					// started, increased
					matched = strings.HasSuffix(e.EventType, "_started") || strings.HasSuffix(e.EventType, "_increased")
				case "resolved":
					// stopped, recovered, decreased
					matched = strings.HasSuffix(e.EventType, "_stopped") || strings.HasSuffix(e.EventType, "_recovered") || strings.HasSuffix(e.EventType, "_decreased")
				}
				if matched {
					filtered = append(filtered, e)
					break
				}
			}
		}
		allEvents = filtered
	}

	// Filter Solana events by DZ connection status
	if params.DZFilter == "on_dz" || params.DZFilter == "off_dz" {
		filtered := make([]TimelineEvent, 0)
		for _, e := range allEvents {
			// Only filter validator and gossip_node events
			if e.EntityType == "validator" || e.EntityType == "gossip_node" {
				// Check if event has ValidatorEventDetails
				if details, ok := e.Details.(ValidatorEventDetails); ok {
					isOnDZ := details.OwnerPubkey != "" || details.DevicePK != ""
					if params.DZFilter == "on_dz" && isOnDZ {
						filtered = append(filtered, e)
					} else if params.DZFilter == "off_dz" && !isOnDZ {
						filtered = append(filtered, e)
					}
				} else {
					// If we can't determine, include based on title
					isOnDZ := strings.HasPrefix(e.Title, "DZ ")
					if params.DZFilter == "on_dz" && isOnDZ {
						filtered = append(filtered, e)
					} else if params.DZFilter == "off_dz" && !isOnDZ {
						filtered = append(filtered, e)
					}
				}
			} else {
				// Non-Solana events pass through
				filtered = append(filtered, e)
			}
		}
		allEvents = filtered
	}

	// Filter by search terms if specified
	if len(params.Search) > 0 {
		filtered := make([]TimelineEvent, 0)
		for _, e := range allEvents {
			if eventMatchesSearch(e, params.Search) {
				filtered = append(filtered, e)
			}
		}
		allEvents = filtered
	}

	// Sort by timestamp descending, then by ID for consistent ordering
	sort.Slice(allEvents, func(i, j int) bool {
		if allEvents[i].Timestamp != allEvents[j].Timestamp {
			return allEvents[i].Timestamp > allEvents[j].Timestamp
		}
		return allEvents[i].ID > allEvents[j].ID
	})

	total := len(allEvents)

	// Apply pagination
	startIdx := params.Offset
	endIdx := params.Offset + params.Limit
	if startIdx > len(allEvents) {
		startIdx = len(allEvents)
	}
	if endIdx > len(allEvents) {
		endIdx = len(allEvents)
	}
	paginatedEvents := allEvents[startIdx:endIdx]

	// Compute histogram from all events (before pagination)
	histogram := computeHistogram(allEvents, params.StartTime, params.EndTime)

	duration := time.Since(start)
	metrics.RecordClickHouseQuery(duration, nil)

	resp := TimelineResponse{
		Events: paginatedEvents,
		Total:  total,
		Limit:  params.Limit,
		Offset: params.Offset,
		TimeRange: TimeRange{
			Start: params.StartTime.Format(time.RFC3339),
			End:   params.EndTime.Format(time.RFC3339),
		},
		Histogram: histogram,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		log.Printf("Error encoding timeline response: %v", err)
	}
}

// computeHistogram creates time buckets from events for visualization
func computeHistogram(events []TimelineEvent, startTime, endTime time.Time) []HistogramBucket {
	if len(events) == 0 {
		return nil
	}

	// Calculate bucket size based on time range
	// Aim for ~24-48 buckets depending on range
	duration := endTime.Sub(startTime)
	var bucketDuration time.Duration
	switch {
	case duration <= 2*time.Hour:
		bucketDuration = 5 * time.Minute
	case duration <= 12*time.Hour:
		bucketDuration = 15 * time.Minute
	case duration <= 24*time.Hour:
		bucketDuration = 30 * time.Minute
	case duration <= 3*24*time.Hour:
		bucketDuration = 2 * time.Hour
	default:
		bucketDuration = 6 * time.Hour
	}

	// Create bucket map
	bucketCounts := make(map[time.Time]int)

	// Count events per bucket
	for _, event := range events {
		ts, err := time.Parse(time.RFC3339, event.Timestamp)
		if err != nil {
			continue
		}
		// Round down to bucket start
		bucketStart := ts.Truncate(bucketDuration)
		bucketCounts[bucketStart]++
	}

	// Generate all buckets in range (including empty ones)
	var buckets []HistogramBucket
	for t := startTime.Truncate(bucketDuration); !t.After(endTime); t = t.Add(bucketDuration) {
		buckets = append(buckets, HistogramBucket{
			Timestamp: t.Format(time.RFC3339),
			Count:     bucketCounts[t],
		})
	}

	return buckets
}

func queryDeviceChanges(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	query := `
		WITH min_ts AS (
			SELECT min(snapshot_ts) as ts FROM dim_dz_devices_history
		),
		all_history AS (
			SELECT
				entity_id,
				snapshot_ts,
				pk,
				code,
				status,
				device_type,
				public_ip,
				contributor_pk,
				metro_pk,
				max_users,
				is_deleted,
				attrs_hash,
				row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as row_num,
				lag(status) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_status,
				lag(device_type) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_device_type,
				lag(public_ip) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_public_ip,
				lag(contributor_pk) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_contributor_pk,
				lag(metro_pk) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_metro_pk,
				lag(max_users) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_max_users,
				lag(attrs_hash) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_attrs_hash,
				lag(is_deleted) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_is_deleted
			FROM dim_dz_devices_history
		)
		SELECT
			h.entity_id,
			h.snapshot_ts,
			h.pk,
			h.code,
			h.status,
			h.device_type,
			h.public_ip,
			h.contributor_pk,
			h.metro_pk,
			h.max_users,
			h.is_deleted,
			h.prev_status,
			h.prev_device_type,
			h.prev_public_ip,
			h.prev_contributor_pk,
			h.prev_metro_pk,
			h.prev_max_users,
			h.prev_is_deleted,
			CASE
				WHEN h.row_num = 1 THEN 'created'
				WHEN h.is_deleted = 1 AND h.prev_is_deleted = 0 THEN 'deleted'
				ELSE 'updated'
			END as change_type,
			COALESCE(c.code, '') as contributor_code,
			COALESCE(m.code, '') as metro_code
		FROM all_history h
		CROSS JOIN min_ts
		LEFT JOIN dz_contributors_current c ON h.contributor_pk = c.pk
		LEFT JOIN dz_metros_current m ON h.metro_pk = m.pk
		WHERE (h.attrs_hash != h.prev_attrs_hash OR h.row_num = 1)
		  AND h.snapshot_ts >= ? AND h.snapshot_ts <= ?
		  -- Exclude initial ingestion events (created at the earliest snapshot time)
		  AND NOT (h.row_num = 1 AND h.snapshot_ts = min_ts.ts)
		ORDER BY h.snapshot_ts DESC
		LIMIT 200
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			entityID          string
			snapshotTS        time.Time
			pk                string
			code              string
			status            string
			deviceType        string
			publicIP          string
			contributorPK     string
			metroPK           string
			maxUsers          int32
			isDeleted         uint8
			prevStatus        *string
			prevDeviceType    *string
			prevPublicIP      *string
			prevContributorPK *string
			prevMetroPK       *string
			prevMaxUsers      *int32
			prevIsDeleted     *uint8
			changeType        string
			contributorCode   string
			metroCode         string
		)

		if err := rows.Scan(
			&entityID, &snapshotTS, &pk, &code, &status, &deviceType, &publicIP,
			&contributorPK, &metroPK, &maxUsers, &isDeleted,
			&prevStatus, &prevDeviceType, &prevPublicIP, &prevContributorPK,
			&prevMetroPK, &prevMaxUsers, &prevIsDeleted, &changeType,
			&contributorCode, &metroCode,
		); err != nil {
			return nil, fmt.Errorf("device scan error: %w", err)
		}

		// Build changes list for updates
		var changes []FieldChange
		if changeType == "updated" {
			if prevStatus != nil && *prevStatus != status {
				changes = append(changes, FieldChange{Field: "status", OldValue: *prevStatus, NewValue: status})
			}
			if prevDeviceType != nil && *prevDeviceType != deviceType {
				changes = append(changes, FieldChange{Field: "device_type", OldValue: *prevDeviceType, NewValue: deviceType})
			}
			if prevPublicIP != nil && *prevPublicIP != publicIP {
				changes = append(changes, FieldChange{Field: "public_ip", OldValue: *prevPublicIP, NewValue: publicIP})
			}
			if prevContributorPK != nil && *prevContributorPK != contributorPK {
				changes = append(changes, FieldChange{Field: "contributor", OldValue: *prevContributorPK, NewValue: contributorPK})
			}
			if prevMetroPK != nil && *prevMetroPK != metroPK {
				changes = append(changes, FieldChange{Field: "metro", OldValue: *prevMetroPK, NewValue: metroPK})
			}
			if prevMaxUsers != nil && *prevMaxUsers != maxUsers {
				changes = append(changes, FieldChange{Field: "max_users", OldValue: *prevMaxUsers, NewValue: maxUsers})
			}
		}

		var title string
		var severity string
		var eventType string

		switch changeType {
		case "created":
			title = fmt.Sprintf("Device %s created", code)
			severity = "info"
			eventType = "entity_created"
		case "deleted":
			title = fmt.Sprintf("Device %s deleted", code)
			severity = "warning"
			eventType = "entity_deleted"
		default:
			if len(changes) == 1 {
				// Single field change - make title more specific
				c := changes[0]
				switch c.Field {
				case "status":
					if status == "activated" {
						title = fmt.Sprintf("Device %s activated", code)
					} else if status == "disabled" {
						title = fmt.Sprintf("Device %s disabled", code)
						severity = "warning"
					} else {
						title = fmt.Sprintf("Device %s status: %s → %s", code, c.OldValue, c.NewValue)
					}
				default:
					title = fmt.Sprintf("Device %s %s changed", code, c.Field)
				}
			} else {
				title = fmt.Sprintf("Device %s updated (%d fields)", code, len(changes))
			}
			if severity == "" {
				severity = "info"
			}
			eventType = "entity_updated"
		}

		entity := DeviceEntity{
			PK:              pk,
			Code:            code,
			Status:          status,
			DeviceType:      deviceType,
			PublicIP:        publicIP,
			ContributorPK:   contributorPK,
			MetroPK:         metroPK,
			MaxUsers:        maxUsers,
			ContributorCode: contributorCode,
			MetroCode:       metroCode,
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(entityID, snapshotTS, eventType),
			EventType:   eventType,
			Timestamp:   snapshotTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    severity,
			Title:       title,
			EntityType:  "device",
			EntityPK:    pk,
			EntityCode:  code,
			Details: EntityChangeDetails{
				ChangeType: changeType,
				Changes:    changes,
				Entity:     entity,
			},
		})
	}

	return events, nil
}

func queryLinkChanges(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	query := `
		WITH min_ts AS (
			SELECT min(snapshot_ts) as ts FROM dim_dz_links_history
		),
		all_history AS (
			SELECT
				entity_id,
				snapshot_ts,
				pk,
				code,
				status,
				link_type,
				tunnel_net,
				contributor_pk,
				side_a_pk,
				side_z_pk,
				side_a_iface_name,
				side_z_iface_name,
				committed_rtt_ns,
				committed_jitter_ns,
				bandwidth_bps,
				isis_delay_override_ns,
				is_deleted,
				attrs_hash,
				row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as row_num,
				lag(status) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_status,
				lag(link_type) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_link_type,
				lag(tunnel_net) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_tunnel_net,
				lag(contributor_pk) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_contributor_pk,
				lag(side_a_pk) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_side_a_pk,
				lag(side_z_pk) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_side_z_pk,
				lag(committed_rtt_ns) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_committed_rtt_ns,
				lag(committed_jitter_ns) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_committed_jitter_ns,
				lag(bandwidth_bps) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_bandwidth_bps,
				lag(isis_delay_override_ns) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_isis_delay_override_ns,
				lag(attrs_hash) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_attrs_hash,
				lag(is_deleted) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_is_deleted
			FROM dim_dz_links_history
		)
		SELECT
			h.entity_id,
			h.snapshot_ts,
			h.pk,
			h.code,
			h.status,
			h.link_type,
			h.tunnel_net,
			h.contributor_pk,
			h.side_a_pk,
			h.side_z_pk,
			h.side_a_iface_name,
			h.side_z_iface_name,
			h.committed_rtt_ns,
			h.committed_jitter_ns,
			h.bandwidth_bps,
			h.isis_delay_override_ns,
			h.is_deleted,
			h.prev_status,
			h.prev_link_type,
			h.prev_tunnel_net,
			h.prev_contributor_pk,
			h.prev_side_a_pk,
			h.prev_side_z_pk,
			h.prev_committed_rtt_ns,
			h.prev_committed_jitter_ns,
			h.prev_bandwidth_bps,
			h.prev_isis_delay_override_ns,
			h.prev_is_deleted,
			CASE
				WHEN h.row_num = 1 THEN 'created'
				WHEN h.is_deleted = 1 AND h.prev_is_deleted = 0 THEN 'deleted'
				ELSE 'updated'
			END as change_type,
			COALESCE(c.code, '') as contributor_code,
			COALESCE(da.code, '') as side_a_code,
			COALESCE(dz.code, '') as side_z_code,
			COALESCE(ma.code, '') as side_a_metro_code,
			COALESCE(mz.code, '') as side_z_metro_code,
			COALESCE(ma.pk, '') as side_a_metro_pk,
			COALESCE(mz.pk, '') as side_z_metro_pk
		FROM all_history h
		CROSS JOIN min_ts
		LEFT JOIN dz_contributors_current c ON h.contributor_pk = c.pk
		LEFT JOIN dz_devices_current da ON h.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON h.side_z_pk = dz.pk
		LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
		LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
		WHERE (h.attrs_hash != h.prev_attrs_hash OR h.row_num = 1)
		  AND h.snapshot_ts >= ? AND h.snapshot_ts <= ?
		  -- Exclude initial ingestion events (created at the earliest snapshot time)
		  AND NOT (h.row_num = 1 AND h.snapshot_ts = min_ts.ts)
		ORDER BY h.snapshot_ts DESC
		LIMIT 200
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			entityID            string
			snapshotTS          time.Time
			pk                  string
			code                string
			status              string
			linkType            string
			tunnelNet           string
			contributorPK       string
			sideAPK             string
			sideZPK             string
			sideAIfaceName      string
			sideZIfaceName      string
			committedRttNs      int64
			committedJitterNs   int64
			bandwidthBps        int64
			isisDelayOverride   int64
			isDeleted           uint8
			prevStatus          *string
			prevLinkType        *string
			prevTunnelNet       *string
			prevContributorPK   *string
			prevSideAPK         *string
			prevSideZPK         *string
			prevCommittedRttNs    *int64
			prevCommittedJitter   *int64
			prevBandwidthBps      *int64
			prevISISDelayOverride *int64
			prevIsDeleted         *uint8
			changeType          string
			contributorCode     string
			sideACode           string
			sideZCode           string
			sideAMetroCode      string
			sideZMetroCode      string
			sideAMetroPK        string
			sideZMetroPK        string
		)

		if err := rows.Scan(
			&entityID, &snapshotTS, &pk, &code, &status, &linkType, &tunnelNet,
			&contributorPK, &sideAPK, &sideZPK, &sideAIfaceName, &sideZIfaceName,
			&committedRttNs, &committedJitterNs, &bandwidthBps, &isisDelayOverride, &isDeleted,
			&prevStatus, &prevLinkType, &prevTunnelNet, &prevContributorPK,
			&prevSideAPK, &prevSideZPK, &prevCommittedRttNs, &prevCommittedJitter,
			&prevBandwidthBps, &prevISISDelayOverride, &prevIsDeleted, &changeType,
			&contributorCode, &sideACode, &sideZCode, &sideAMetroCode, &sideZMetroCode,
			&sideAMetroPK, &sideZMetroPK,
		); err != nil {
			return nil, fmt.Errorf("link scan error: %w", err)
		}

		// Build changes list for updates
		var changes []FieldChange
		if changeType == "updated" {
			if prevStatus != nil && *prevStatus != status {
				changes = append(changes, FieldChange{Field: "status", OldValue: *prevStatus, NewValue: status})
			}
			if prevLinkType != nil && *prevLinkType != linkType {
				changes = append(changes, FieldChange{Field: "link_type", OldValue: *prevLinkType, NewValue: linkType})
			}
			if prevTunnelNet != nil && *prevTunnelNet != tunnelNet {
				changes = append(changes, FieldChange{Field: "tunnel_net", OldValue: *prevTunnelNet, NewValue: tunnelNet})
			}
			if prevContributorPK != nil && *prevContributorPK != contributorPK {
				changes = append(changes, FieldChange{Field: "contributor", OldValue: *prevContributorPK, NewValue: contributorPK})
			}
			if prevSideAPK != nil && *prevSideAPK != sideAPK {
				changes = append(changes, FieldChange{Field: "side_a", OldValue: *prevSideAPK, NewValue: sideAPK})
			}
			if prevSideZPK != nil && *prevSideZPK != sideZPK {
				changes = append(changes, FieldChange{Field: "side_z", OldValue: *prevSideZPK, NewValue: sideZPK})
			}
			if prevCommittedRttNs != nil && *prevCommittedRttNs != committedRttNs {
				changes = append(changes, FieldChange{Field: "committed_rtt", OldValue: *prevCommittedRttNs, NewValue: committedRttNs})
			}
			if prevCommittedJitter != nil && *prevCommittedJitter != committedJitterNs {
				changes = append(changes, FieldChange{Field: "committed_jitter", OldValue: *prevCommittedJitter, NewValue: committedJitterNs})
			}
			if prevBandwidthBps != nil && *prevBandwidthBps != bandwidthBps {
				changes = append(changes, FieldChange{Field: "bandwidth", OldValue: *prevBandwidthBps, NewValue: bandwidthBps})
			}
			if prevISISDelayOverride != nil && *prevISISDelayOverride != isisDelayOverride {
				changes = append(changes, FieldChange{Field: "isis_delay_override", OldValue: *prevISISDelayOverride, NewValue: isisDelayOverride})
			}
		}

		var title string
		var severity string
		var eventType string

		switch changeType {
		case "created":
			title = fmt.Sprintf("Link %s created", code)
			severity = "info"
			eventType = "entity_created"
		case "deleted":
			title = fmt.Sprintf("Link %s deleted", code)
			severity = "warning"
			eventType = "entity_deleted"
		default:
			if len(changes) == 1 {
				c := changes[0]
				switch c.Field {
				case "status":
					if status == "activated" {
						title = fmt.Sprintf("Link %s activated", code)
					} else if status == "disabled" {
						title = fmt.Sprintf("Link %s disabled", code)
						severity = "warning"
					} else {
						title = fmt.Sprintf("Link %s status: %s → %s", code, c.OldValue, c.NewValue)
					}
				case "bandwidth":
					title = fmt.Sprintf("Link %s bandwidth changed", code)
				case "committed_rtt":
					title = fmt.Sprintf("Link %s committed RTT changed", code)
				case "isis_delay_override":
					title = fmt.Sprintf("Link %s ISIS delay override changed", code)
				default:
					title = fmt.Sprintf("Link %s %s changed", code, c.Field)
				}
			} else {
				title = fmt.Sprintf("Link %s updated (%d fields)", code, len(changes))
			}
			if severity == "" {
				severity = "info"
			}
			eventType = "entity_updated"
		}

		entity := LinkEntity{
			PK:                pk,
			Code:              code,
			Status:            status,
			LinkType:          linkType,
			TunnelNet:         tunnelNet,
			ContributorPK:     contributorPK,
			SideAPK:           sideAPK,
			SideZPK:           sideZPK,
			SideAIfaceName:    sideAIfaceName,
			SideZIfaceName:    sideZIfaceName,
			CommittedRttNs:    committedRttNs,
			CommittedJitterNs: committedJitterNs,
			BandwidthBps:      bandwidthBps,
			ISISDelayOverride: isisDelayOverride,
			ContributorCode:   contributorCode,
			SideACode:         sideACode,
			SideZCode:         sideZCode,
			SideAMetroCode:    sideAMetroCode,
			SideZMetroCode:    sideZMetroCode,
			SideAMetroPK:      sideAMetroPK,
			SideZMetroPK:      sideZMetroPK,
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(entityID, snapshotTS, eventType),
			EventType:   eventType,
			Timestamp:   snapshotTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    severity,
			Title:       title,
			EntityType:  "link",
			EntityPK:    pk,
			EntityCode:  code,
			Details: EntityChangeDetails{
				ChangeType: changeType,
				Changes:    changes,
				Entity:     entity,
			},
		})
	}

	return events, nil
}

func queryMetroChanges(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	query := `
		WITH min_ts AS (
			SELECT min(snapshot_ts) as ts FROM dim_dz_metros_history
		),
		all_history AS (
			SELECT
				entity_id,
				snapshot_ts,
				pk,
				code,
				name,
				longitude,
				latitude,
				is_deleted,
				attrs_hash,
				row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as row_num,
				lag(name) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_name,
				lag(longitude) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_longitude,
				lag(latitude) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_latitude,
				lag(attrs_hash) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_attrs_hash,
				lag(is_deleted) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_is_deleted
			FROM dim_dz_metros_history
		)
		SELECT
			h.entity_id,
			h.snapshot_ts,
			h.pk,
			h.code,
			h.name,
			h.longitude,
			h.latitude,
			h.is_deleted,
			h.prev_name,
			h.prev_longitude,
			h.prev_latitude,
			h.prev_is_deleted,
			CASE
				WHEN h.row_num = 1 THEN 'created'
				WHEN h.is_deleted = 1 AND h.prev_is_deleted = 0 THEN 'deleted'
				ELSE 'updated'
			END as change_type
		FROM all_history h
		CROSS JOIN min_ts
		WHERE (h.attrs_hash != h.prev_attrs_hash OR h.row_num = 1)
		  AND h.snapshot_ts >= ? AND h.snapshot_ts <= ?
		  -- Exclude initial ingestion events (created at the earliest snapshot time)
		  AND NOT (h.row_num = 1 AND h.snapshot_ts = min_ts.ts)
		ORDER BY h.snapshot_ts DESC
		LIMIT 100
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			entityID      string
			snapshotTS    time.Time
			pk            string
			code          string
			name          string
			longitude     float64
			latitude      float64
			isDeleted     uint8
			prevName      *string
			prevLongitude *float64
			prevLatitude  *float64
			prevIsDeleted *uint8
			changeType    string
		)

		if err := rows.Scan(&entityID, &snapshotTS, &pk, &code, &name, &longitude, &latitude, &isDeleted, &prevName, &prevLongitude, &prevLatitude, &prevIsDeleted, &changeType); err != nil {
			return nil, fmt.Errorf("metro scan error: %w", err)
		}

		// Build changes list
		var changes []FieldChange
		if changeType == "updated" {
			if prevName != nil && *prevName != name {
				changes = append(changes, FieldChange{Field: "name", OldValue: *prevName, NewValue: name})
			}
			if prevLongitude != nil && *prevLongitude != longitude {
				changes = append(changes, FieldChange{Field: "longitude", OldValue: *prevLongitude, NewValue: longitude})
			}
			if prevLatitude != nil && *prevLatitude != latitude {
				changes = append(changes, FieldChange{Field: "latitude", OldValue: *prevLatitude, NewValue: latitude})
			}
		}

		var title string
		var eventType string

		switch changeType {
		case "created":
			title = fmt.Sprintf("Metro %s added", code)
			eventType = "entity_created"
		case "deleted":
			title = fmt.Sprintf("Metro %s removed", code)
			eventType = "entity_deleted"
		default:
			if len(changes) == 1 {
				c := changes[0]
				if c.Field == "name" {
					title = fmt.Sprintf("Metro %s renamed: %s → %s", code, c.OldValue, c.NewValue)
				} else {
					title = fmt.Sprintf("Metro %s %s changed", code, c.Field)
				}
			} else {
				title = fmt.Sprintf("Metro %s updated (%d fields)", code, len(changes))
			}
			eventType = "entity_updated"
		}

		entity := MetroEntity{
			PK:        pk,
			Code:      code,
			Name:      name,
			Longitude: longitude,
			Latitude:  latitude,
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(entityID, snapshotTS, eventType),
			EventType:   eventType,
			Timestamp:   snapshotTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    "info",
			Title:       title,
			EntityType:  "metro",
			EntityPK:    pk,
			EntityCode:  code,
			Details: EntityChangeDetails{
				ChangeType: changeType,
				Changes:    changes,
				Entity:     entity,
			},
		})
	}

	return events, nil
}

func queryContributorChanges(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	query := `
		WITH min_ts AS (
			SELECT min(snapshot_ts) as ts FROM dim_dz_contributors_history
		),
		all_history AS (
			SELECT
				entity_id,
				snapshot_ts,
				pk,
				code,
				name,
				is_deleted,
				attrs_hash,
				row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as row_num,
				lag(name) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_name,
				lag(code) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_code,
				lag(attrs_hash) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_attrs_hash,
				lag(is_deleted) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_is_deleted
			FROM dim_dz_contributors_history
		)
		SELECT
			h.entity_id,
			h.snapshot_ts,
			h.pk,
			h.code,
			h.name,
			h.is_deleted,
			h.prev_code,
			h.prev_name,
			h.prev_is_deleted,
			CASE
				WHEN h.row_num = 1 THEN 'created'
				WHEN h.is_deleted = 1 AND h.prev_is_deleted = 0 THEN 'deleted'
				ELSE 'updated'
			END as change_type
		FROM all_history h
		CROSS JOIN min_ts
		WHERE (h.attrs_hash != h.prev_attrs_hash OR h.row_num = 1)
		  AND h.snapshot_ts >= ? AND h.snapshot_ts <= ?
		  -- Exclude initial ingestion events (created at the earliest snapshot time)
		  AND NOT (h.row_num = 1 AND h.snapshot_ts = min_ts.ts)
		ORDER BY h.snapshot_ts DESC
		LIMIT 100
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			entityID      string
			snapshotTS    time.Time
			pk            string
			code          string
			name          string
			isDeleted     uint8
			prevCode      *string
			prevName      *string
			prevIsDeleted *uint8
			changeType    string
		)

		if err := rows.Scan(&entityID, &snapshotTS, &pk, &code, &name, &isDeleted, &prevCode, &prevName, &prevIsDeleted, &changeType); err != nil {
			return nil, fmt.Errorf("contributor scan error: %w", err)
		}

		// Build changes list
		var changes []FieldChange
		if changeType == "updated" {
			if prevCode != nil && *prevCode != code {
				changes = append(changes, FieldChange{Field: "code", OldValue: *prevCode, NewValue: code})
			}
			if prevName != nil && *prevName != name {
				changes = append(changes, FieldChange{Field: "name", OldValue: *prevName, NewValue: name})
			}
		}

		var title string
		var eventType string

		switch changeType {
		case "created":
			title = fmt.Sprintf("Contributor %s added", code)
			eventType = "entity_created"
		case "deleted":
			title = fmt.Sprintf("Contributor %s removed", code)
			eventType = "entity_deleted"
		default:
			if len(changes) == 1 {
				c := changes[0]
				if c.Field == "name" {
					title = fmt.Sprintf("Contributor %s renamed: %s → %s", code, c.OldValue, c.NewValue)
				} else if c.Field == "code" {
					title = fmt.Sprintf("Contributor code changed: %s → %s", c.OldValue, c.NewValue)
				} else {
					title = fmt.Sprintf("Contributor %s %s changed", code, c.Field)
				}
			} else {
				title = fmt.Sprintf("Contributor %s updated (%d fields)", code, len(changes))
			}
			eventType = "entity_updated"
		}

		entity := ContributorEntity{
			PK:   pk,
			Code: code,
			Name: name,
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(entityID, snapshotTS, eventType),
			EventType:   eventType,
			Timestamp:   snapshotTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    "info",
			Title:       title,
			EntityType:  "contributor",
			EntityPK:    pk,
			EntityCode:  code,
			Details: EntityChangeDetails{
				ChangeType: changeType,
				Changes:    changes,
				Entity:     entity,
			},
		})
	}

	return events, nil
}

func queryUserChanges(ctx context.Context, startTime, endTime time.Time, includeInternal bool) ([]TimelineEvent, error) {
	// Build internal user filter
	internalFilter := ""
	if !includeInternal && len(internalUserPubkeys) > 0 {
		internalFilter = fmt.Sprintf(" AND owner_pubkey NOT IN ('%s')", strings.Join(internalUserPubkeys, "','"))
	}

	query := fmt.Sprintf(`
		WITH min_ts AS (
			SELECT min(snapshot_ts) as ts FROM dim_dz_users_history
		),
		all_history AS (
			SELECT
				entity_id,
				snapshot_ts,
				pk,
				owner_pubkey,
				kind,
				status,
				client_ip,
				dz_ip,
				device_pk,
				tunnel_id,
				is_deleted,
				attrs_hash,
				row_number() OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as row_num,
				lag(status) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_status,
				lag(kind) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_kind,
				lag(client_ip) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_client_ip,
				lag(dz_ip) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_dz_ip,
				lag(device_pk) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_device_pk,
				lag(tunnel_id) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_tunnel_id,
				lag(attrs_hash) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_attrs_hash,
				lag(is_deleted) OVER (PARTITION BY entity_id ORDER BY snapshot_ts, ingested_at, op_id) as prev_is_deleted
			FROM dim_dz_users_history
			WHERE kind NOT IN ('validator', 'gossip_only')%s
		)
		SELECT
			h.entity_id,
			h.snapshot_ts,
			h.pk,
			h.owner_pubkey,
			h.kind,
			h.status,
			h.client_ip,
			h.dz_ip,
			h.device_pk,
			h.tunnel_id,
			h.is_deleted,
			h.prev_status,
			h.prev_kind,
			h.prev_client_ip,
			h.prev_dz_ip,
			h.prev_device_pk,
			h.prev_tunnel_id,
			h.prev_is_deleted,
			CASE
				WHEN h.row_num = 1 THEN 'created'
				WHEN h.is_deleted = 1 AND h.prev_is_deleted = 0 THEN 'deleted'
				ELSE 'updated'
			END as change_type,
			COALESCE(d.code, '') as device_code,
			COALESCE(m.code, '') as metro_code
		FROM all_history h
		CROSS JOIN min_ts
		LEFT JOIN dz_devices_current d ON h.device_pk = d.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		WHERE (h.attrs_hash != h.prev_attrs_hash OR h.row_num = 1)
		  AND h.snapshot_ts >= ? AND h.snapshot_ts <= ?
		  -- Exclude initial ingestion events (created at the earliest snapshot time)
		  AND NOT (h.row_num = 1 AND h.snapshot_ts = min_ts.ts)
		ORDER BY h.snapshot_ts DESC
		LIMIT 200
	`, internalFilter)

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			entityID     string
			snapshotTS   time.Time
			pk           string
			ownerPubkey  string
			kind         string
			status       string
			clientIP     string
			dzIP         string
			devicePK     string
			tunnelID     int32
			isDeleted    uint8
			prevStatus   *string
			prevKind     *string
			prevClientIP *string
			prevDZIP     *string
			prevDevicePK *string
			prevTunnelID *int32
			prevIsDeleted *uint8
			changeType   string
			deviceCode   string
			metroCode    string
		)

		if err := rows.Scan(
			&entityID, &snapshotTS, &pk, &ownerPubkey, &kind, &status, &clientIP, &dzIP,
			&devicePK, &tunnelID, &isDeleted,
			&prevStatus, &prevKind, &prevClientIP, &prevDZIP, &prevDevicePK, &prevTunnelID, &prevIsDeleted,
			&changeType, &deviceCode, &metroCode,
		); err != nil {
			return nil, fmt.Errorf("user scan error: %w", err)
		}

		// Build changes list
		var changes []FieldChange
		if changeType == "updated" {
			if prevStatus != nil && *prevStatus != status {
				changes = append(changes, FieldChange{Field: "status", OldValue: *prevStatus, NewValue: status})
			}
			if prevKind != nil && *prevKind != kind {
				changes = append(changes, FieldChange{Field: "kind", OldValue: *prevKind, NewValue: kind})
			}
			if prevClientIP != nil && *prevClientIP != clientIP {
				changes = append(changes, FieldChange{Field: "client_ip", OldValue: *prevClientIP, NewValue: clientIP})
			}
			if prevDZIP != nil && *prevDZIP != dzIP {
				changes = append(changes, FieldChange{Field: "dz_ip", OldValue: *prevDZIP, NewValue: dzIP})
			}
			if prevDevicePK != nil && *prevDevicePK != devicePK {
				changes = append(changes, FieldChange{Field: "device", OldValue: *prevDevicePK, NewValue: devicePK})
			}
			if prevTunnelID != nil && *prevTunnelID != tunnelID {
				changes = append(changes, FieldChange{Field: "tunnel_id", OldValue: *prevTunnelID, NewValue: tunnelID})
			}
		}

		var title string
		var description string
		var severity string
		var eventType string
		displayCode := ownerPubkey[:8] + "..."

		switch changeType {
		case "created":
			if deviceCode != "" {
				title = fmt.Sprintf("User %s connected to %s", displayCode, deviceCode)
			} else {
				title = fmt.Sprintf("User %s created", displayCode)
			}
			severity = "info"
			eventType = "entity_created"
		case "deleted":
			if deviceCode != "" {
				title = fmt.Sprintf("User %s disconnected from %s", displayCode, deviceCode)
			} else {
				title = fmt.Sprintf("User %s deleted", displayCode)
			}
			severity = "warning"
			eventType = "entity_deleted"
		default:
			if len(changes) == 1 {
				c := changes[0]
				switch c.Field {
				case "status":
					if status == "activated" {
						title = fmt.Sprintf("User %s activated", displayCode)
					} else if status == "disabled" {
						title = fmt.Sprintf("User %s disabled", displayCode)
						severity = "warning"
					} else {
						title = fmt.Sprintf("User %s status: %s → %s", displayCode, c.OldValue, c.NewValue)
					}
				case "device":
					title = fmt.Sprintf("User %s moved to %s", displayCode, deviceCode)
				default:
					title = fmt.Sprintf("User %s %s changed", displayCode, c.Field)
				}
			} else {
				title = fmt.Sprintf("User %s updated (%d fields)", displayCode, len(changes))
			}
			if severity == "" {
				severity = "info"
			}
			eventType = "entity_updated"
		}

		entity := UserEntity{
			PK:          pk,
			OwnerPubkey: ownerPubkey,
			Status:      status,
			Kind:        kind,
			ClientIP:    clientIP,
			DZIP:        dzIP,
			DevicePK:    devicePK,
			TunnelID:    tunnelID,
			DeviceCode:  deviceCode,
			MetroCode:   metroCode,
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(entityID, snapshotTS, eventType),
			EventType:   eventType,
			Timestamp:   snapshotTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    severity,
			Title:       title,
			Description: description,
			EntityType:  "user",
			EntityPK:    pk,
			EntityCode:  ownerPubkey, // Full pubkey - frontend handles truncation
			Details: EntityChangeDetails{
				ChangeType: changeType,
				Changes:    changes,
				Entity:     entity,
			},
		})
	}

	return events, nil
}

func queryPacketLossEvents(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	// Detect packet loss transitions (started/recovered) using hourly aggregates
	// Look back 1 hour to detect transitions at the boundary
	lookbackStart := startTime.Add(-1 * time.Hour)

	query := `
		WITH hourly_loss AS (
			SELECT
				link_pk,
				toStartOfHour(event_ts) as hour,
				countIf(loss = true OR rtt_us = 0) * 100.0 / count(*) as loss_pct,
				count(*) as samples
			FROM fact_dz_device_link_latency
			WHERE event_ts >= ? AND event_ts <= ?
			GROUP BY link_pk, hour
			HAVING samples >= 10
		),
		with_prev AS (
			SELECT
				link_pk,
				hour,
				loss_pct,
				lag(loss_pct) OVER (PARTITION BY link_pk ORDER BY hour) as prev_loss_pct
			FROM hourly_loss
		),
		transitions AS (
			SELECT
				link_pk,
				hour,
				loss_pct,
				prev_loss_pct,
				CASE
					WHEN loss_pct >= 0.1 AND (prev_loss_pct < 0.1 OR prev_loss_pct IS NULL) THEN 'started'
					WHEN loss_pct < 0.1 AND prev_loss_pct >= 0.1 THEN 'recovered'
				END as transition_type,
				CASE
					WHEN loss_pct >= 1.0 THEN 'critical'
					WHEN loss_pct >= 0.1 THEN 'warning'
					ELSE 'info'
				END as severity
			FROM with_prev
		)
		SELECT
			t.link_pk,
			t.hour,
			t.loss_pct,
			COALESCE(t.prev_loss_pct, 0) as prev_loss_pct,
			t.transition_type,
			t.severity,
			l.code as link_code,
			l.link_type,
			COALESCE(ma.code, '') as side_a_metro,
			COALESCE(mz.code, '') as side_z_metro
		FROM transitions t
		JOIN dz_links_current l ON t.link_pk = l.pk
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
		LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
		WHERE t.transition_type IS NOT NULL
		  AND t.hour >= ?
		ORDER BY t.hour DESC
		LIMIT 200
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, lookbackStart, endTime, startTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			linkPK         string
			hour           time.Time
			lossPct        float64
			prevLossPct    float64
			transitionType string
			severity       string
			linkCode       string
			linkType       string
			sideAMetro     string
			sideZMetro     string
		)

		if err := rows.Scan(&linkPK, &hour, &lossPct, &prevLossPct, &transitionType, &severity, &linkCode, &linkType, &sideAMetro, &sideZMetro); err != nil {
			return nil, fmt.Errorf("packet loss event scan error: %w", err)
		}

		var title string
		var eventType string
		if transitionType == "started" {
			title = fmt.Sprintf("Packet loss started on %s", linkCode)
			eventType = "packet_loss_started"
		} else {
			title = fmt.Sprintf("Packet loss recovered on %s", linkCode)
			eventType = "packet_loss_recovered"
			severity = "success" // Recovery is success (green)
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(linkPK, hour, eventType),
			EventType:   eventType,
			Timestamp:   hour.Format(time.RFC3339),
			Category:    "packet_loss",
			Severity:    severity,
			Title:       title,
			Description: "",
			EntityType:  "link",
			EntityPK:    linkPK,
			EntityCode:  linkCode,
			Details: PacketLossEventDetails{
				LinkPK:          linkPK,
				LinkCode:        linkCode,
				LinkType:        linkType,
				SideAMetro:      sideAMetro,
				SideZMetro:      sideZMetro,
				PreviousLossPct: prevLossPct,
				CurrentLossPct:  lossPct,
				Direction:       transitionType,
			},
		})
	}

	return events, nil
}

func queryInterfaceEvents(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	// Detect interface issue transitions (started/stopped) using hourly aggregates with lag()
	// We need to look at 1 hour before startTime to detect transitions at the boundary
	lookbackStart := startTime.Add(-1 * time.Hour)

	query := `
		WITH hourly AS (
			SELECT
				toStartOfHour(ic.event_ts) as hour,
				ic.device_pk,
				ic.intf,
				any(ic.link_pk) as link_pk,
				sum(COALESCE(ic.in_errors_delta, 0)) as in_errors,
				sum(COALESCE(ic.out_errors_delta, 0)) as out_errors,
				sum(COALESCE(ic.in_discards_delta, 0)) as in_discards,
				sum(COALESCE(ic.out_discards_delta, 0)) as out_discards,
				sum(COALESCE(ic.carrier_transitions_delta, 0)) as carrier_transitions
			FROM fact_dz_device_interface_counters ic
			WHERE ic.event_ts >= ? AND ic.event_ts <= ?
			GROUP BY hour, ic.device_pk, ic.intf
		),
		with_prev AS (
			SELECT
				hour,
				device_pk,
				intf,
				link_pk,
				in_errors,
				out_errors,
				in_discards,
				out_discards,
				carrier_transitions,
				in_errors + out_errors as total_errors,
				in_discards + out_discards as total_discards,
				lag(in_errors + out_errors) OVER (PARTITION BY device_pk, intf ORDER BY hour) as prev_total_errors,
				lag(in_discards + out_discards) OVER (PARTITION BY device_pk, intf ORDER BY hour) as prev_total_discards,
				lag(carrier_transitions) OVER (PARTITION BY device_pk, intf ORDER BY hour) as prev_carrier_transitions
			FROM hourly
		),
		transitions AS (
			SELECT
				hour,
				device_pk,
				intf,
				link_pk,
				in_errors,
				out_errors,
				in_discards,
				out_discards,
				carrier_transitions,
				total_errors,
				total_discards,
				CASE
					WHEN total_errors > 0 AND (prev_total_errors = 0 OR prev_total_errors IS NULL) THEN 'errors_started'
					WHEN total_errors = 0 AND prev_total_errors > 0 THEN 'errors_stopped'
					WHEN total_discards > 0 AND (prev_total_discards = 0 OR prev_total_discards IS NULL) THEN 'discards_started'
					WHEN total_discards = 0 AND prev_total_discards > 0 THEN 'discards_stopped'
					WHEN carrier_transitions > 0 AND (prev_carrier_transitions = 0 OR prev_carrier_transitions IS NULL) THEN 'carrier_started'
					WHEN carrier_transitions = 0 AND prev_carrier_transitions > 0 THEN 'carrier_stopped'
				END as transition_type
			FROM with_prev
		)
		SELECT
			t.hour,
			t.device_pk,
			t.intf,
			t.link_pk,
			t.in_errors,
			t.out_errors,
			t.in_discards,
			t.out_discards,
			t.carrier_transitions,
			t.transition_type,
			d.code as device_code,
			COALESCE(l.code, '') as link_code
		FROM transitions t
		JOIN dz_devices_current d ON t.device_pk = d.pk
		LEFT JOIN dz_links_current l ON t.link_pk = l.pk
		WHERE t.transition_type IS NOT NULL
		  AND t.hour >= ?
		  AND d.status = 'activated'
		ORDER BY t.hour DESC
		LIMIT 200
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, lookbackStart, endTime, startTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			hour               time.Time
			devicePK           string
			intf               string
			linkPK             string
			inErrors           int64
			outErrors          int64
			inDiscards         int64
			outDiscards        int64
			carrierTransitions int64
			transitionType     string
			deviceCode         string
			linkCode           string
		)

		if err := rows.Scan(&hour, &devicePK, &intf, &linkPK, &inErrors, &outErrors, &inDiscards, &outDiscards, &carrierTransitions, &transitionType, &deviceCode, &linkCode); err != nil {
			return nil, fmt.Errorf("interface event scan error: %w", err)
		}

		var title string
		var eventType string
		var issueType string
		var category string
		var severity string

		totalErrors := inErrors + outErrors
		totalDiscards := inDiscards + outDiscards

		// Build interface identifier - include link code if available
		intfDesc := fmt.Sprintf("%s %s", deviceCode, intf)
		if linkCode != "" {
			intfDesc = fmt.Sprintf("%s %s (%s)", deviceCode, intf, linkCode)
		}

		switch transitionType {
		case "errors_started":
			title = fmt.Sprintf("Interface errors started on %s", intfDesc)
			eventType = "interface_errors_started"
			issueType = "errors"
			category = "interface_errors"
			if totalErrors > 100 {
				severity = "critical"
			} else {
				severity = "warning"
			}
		case "errors_stopped":
			title = fmt.Sprintf("Interface errors stopped on %s", intfDesc)
			eventType = "interface_errors_stopped"
			issueType = "errors"
			category = "interface_errors"
			severity = "success"
		case "discards_started":
			title = fmt.Sprintf("Interface discards started on %s", intfDesc)
			eventType = "interface_discards_started"
			issueType = "discards"
			category = "interface_discards"
			if totalDiscards > 1000 {
				severity = "warning"
			} else {
				severity = "info"
			}
		case "discards_stopped":
			title = fmt.Sprintf("Interface discards stopped on %s", intfDesc)
			eventType = "interface_discards_stopped"
			issueType = "discards"
			category = "interface_discards"
			severity = "success"
		case "carrier_started":
			title = fmt.Sprintf("Carrier transitions started on %s", intfDesc)
			eventType = "interface_carrier_started"
			issueType = "carrier"
			category = "interface_carrier"
			severity = "warning"
		case "carrier_stopped":
			title = fmt.Sprintf("Carrier transitions stopped on %s", intfDesc)
			eventType = "interface_carrier_stopped"
			issueType = "carrier"
			category = "interface_carrier"
			severity = "success"
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(devicePK+intf, hour, eventType),
			EventType:   eventType,
			Timestamp:   hour.Format(time.RFC3339),
			Category:    category,
			Severity:    severity,
			Title:       title,
			EntityType:  "device",
			EntityPK:    devicePK,
			EntityCode:  deviceCode,
			Details: InterfaceEventDetails{
				DevicePK:           devicePK,
				DeviceCode:         deviceCode,
				InterfaceName:      intf,
				LinkPK:             linkPK,
				LinkCode:           linkCode,
				InErrors:           inErrors,
				OutErrors:          outErrors,
				InDiscards:         inDiscards,
				OutDiscards:        outDiscards,
				CarrierTransitions: carrierTransitions,
				IssueType:          issueType,
			},
		})
	}

	return events, nil
}

func queryValidatorEvents(ctx context.Context, startTime, endTime time.Time, includeInternal bool) ([]TimelineEvent, error) {
	// Build internal user filter
	internalFilter := ""
	if !includeInternal && len(internalUserPubkeys) > 0 {
		internalFilter = fmt.Sprintf(" AND u.owner_pubkey NOT IN ('%s')", strings.Join(internalUserPubkeys, "','"))
	}

	// Validators/gossip nodes are identified by joining users with gossip_nodes via dz_ip = gossip_ip
	// A user is a "validator" if their gossip node has a vote account, otherwise "gossip_node"
	// We use history tables to detect both join AND leave events (current tables miss users who left)
	query := fmt.Sprintf(`
		WITH total_stake AS (
			SELECT sum(activated_stake_lamports) as total
			FROM solana_vote_accounts_current
		),
		-- Get gossip IPs that were ever active during the time range (using history table)
		gossip_ips AS (
			SELECT DISTINCT gossip_ip
			FROM dim_solana_gossip_nodes_history
			WHERE snapshot_ts >= ? AND snapshot_ts <= ?
		),
		all_history AS (
			SELECT
				u.entity_id,
				u.snapshot_ts,
				u.pk,
				u.owner_pubkey,
				u.kind,
				u.status,
				u.dz_ip,
				u.device_pk,
				u.attrs_hash,
				row_number() OVER (PARTITION BY u.entity_id ORDER BY u.snapshot_ts, u.ingested_at, u.op_id) as row_num,
				lag(u.status) OVER (PARTITION BY u.entity_id ORDER BY u.snapshot_ts, u.ingested_at, u.op_id) as prev_status,
				lag(u.attrs_hash) OVER (PARTITION BY u.entity_id ORDER BY u.snapshot_ts, u.ingested_at, u.op_id) as prev_attrs_hash
			FROM dim_dz_users_history u
			-- Include users whose dz_ip was in the gossip nodes during the time range
			WHERE u.dz_ip IN (SELECT gossip_ip FROM gossip_ips)%s
		),
		-- Get latest gossip node info for each IP from history (for validators who may have left)
		latest_gossip AS (
			SELECT gossip_ip, argMax(pubkey, snapshot_ts) as pubkey
			FROM dim_solana_gossip_nodes_history
			WHERE snapshot_ts >= ? AND snapshot_ts <= ?
			GROUP BY gossip_ip
		),
		-- Get latest vote account info for each node from history
		latest_vote AS (
			SELECT node_pubkey, vote_pubkey, argMax(activated_stake_lamports, snapshot_ts) as stake_lamports
			FROM dim_solana_vote_accounts_history
			WHERE snapshot_ts >= ? AND snapshot_ts <= ?
			GROUP BY node_pubkey, vote_pubkey
		)
		SELECT
			uc.entity_id,
			uc.snapshot_ts,
			uc.pk,
			uc.owner_pubkey,
			uc.kind,
			uc.status,
			uc.prev_status,
			COALESCE(uc.dz_ip, '') as dz_ip,
			COALESCE(uc.device_pk, '') as device_pk,
			COALESCE(d.code, '') as device_code,
			COALESCE(m.code, '') as metro_code,
			-- Use current gossip info if available, fall back to historical
			COALESCE(gn_curr.pubkey, gn_hist.pubkey, '') as node_pubkey,
			COALESCE(va_curr.vote_pubkey, va_hist.vote_pubkey, '') as vote_pubkey,
			COALESCE(va_curr.activated_stake_lamports, va_hist.stake_lamports, 0) as stake_lamports,
			COALESCE(va_curr.activated_stake_lamports, va_hist.stake_lamports, 0) * 100.0 / NULLIF(ts.total, 0) as stake_share_pct,
			CASE WHEN COALESCE(va_curr.vote_pubkey, va_hist.vote_pubkey, '') != '' THEN 'validator' ELSE 'gossip_only' END as validator_kind
		FROM all_history uc
		CROSS JOIN total_stake ts
		LEFT JOIN dz_devices_current d ON uc.device_pk = d.pk
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		-- Current gossip/vote info (for active validators)
		LEFT JOIN solana_gossip_nodes_current gn_curr ON uc.dz_ip = gn_curr.gossip_ip
		LEFT JOIN solana_vote_accounts_current va_curr ON gn_curr.pubkey = va_curr.node_pubkey
		-- Historical gossip/vote info (for validators who have left)
		LEFT JOIN latest_gossip gn_hist ON uc.dz_ip = gn_hist.gossip_ip
		LEFT JOIN latest_vote va_hist ON gn_hist.pubkey = va_hist.node_pubkey
		WHERE (uc.attrs_hash != uc.prev_attrs_hash OR uc.row_num = 1)
		  AND ((uc.status = 'activated' AND (uc.prev_status != 'activated' OR uc.row_num = 1))
		       OR (uc.status != 'activated' AND uc.prev_status = 'activated'))
		  AND uc.snapshot_ts >= ? AND uc.snapshot_ts <= ?
		ORDER BY uc.snapshot_ts DESC
		LIMIT 200
	`, internalFilter)

	start := time.Now()
	// Query has 4 pairs of time parameters: gossip_ips, latest_gossip, latest_vote, and final WHERE
	rows, err := config.DB.Query(ctx, query,
		startTime, endTime, // gossip_ips CTE
		startTime, endTime, // latest_gossip CTE
		startTime, endTime, // latest_vote CTE
		startTime, endTime, // final WHERE clause
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			entityID      string
			snapshotTS    time.Time
			pk            string
			ownerPubkey   string
			kind          string
			status        string
			prevStatus    *string
			dzIP          string
			devicePK      string
			deviceCode    string
			metroCode     string
			nodePubkey    string
			votePubkey    string
			stakeLamports int64
			stakeSharePct float64
			validatorKind string // "validator" or "gossip_only" based on vote account presence
		)

		if err := rows.Scan(&entityID, &snapshotTS, &pk, &ownerPubkey, &kind, &status, &prevStatus, &dzIP, &devicePK, &deviceCode, &metroCode, &nodePubkey, &votePubkey, &stakeLamports, &stakeSharePct, &validatorKind); err != nil {
			return nil, fmt.Errorf("validator event scan error: %w", err)
		}

		var title string
		var action string
		var eventType string
		var severity string

		stakeSol := float64(stakeLamports) / 1_000_000_000
		entityTypeStr := "validator"
		if validatorKind == "gossip_only" {
			entityTypeStr = "gossip_node"
		}

		isJoining := status == "activated" && (prevStatus == nil || *prevStatus != "activated")

		if isJoining {
			action = "joined"
			if validatorKind == "validator" {
				if stakeSol > 0 {
					title = fmt.Sprintf("Validator joined DZ (%.0f SOL, %.2f%%)", stakeSol, stakeSharePct)
				} else {
					title = "Validator joined DZ"
				}
				eventType = "validator_joined"
			} else {
				title = "Gossip node joined DZ"
				eventType = "gossip_node_joined"
			}
			severity = "info"
		} else {
			action = "left"
			if validatorKind == "validator" {
				if stakeSol > 0 {
					title = fmt.Sprintf("Validator left DZ (%.0f SOL, %.2f%%)", stakeSol, stakeSharePct)
				} else {
					title = "Validator left DZ"
				}
				eventType = "validator_left"
			} else {
				title = "Gossip node left DZ"
				eventType = "gossip_node_left"
			}
			severity = "warning"
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(entityID, snapshotTS, eventType),
			EventType:   eventType,
			Timestamp:   snapshotTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    severity,
			Title:       title,
			Description: "",
			EntityType:  entityTypeStr,
			EntityPK:    pk,
			EntityCode:  ownerPubkey, // Full pubkey - frontend handles truncation
			Details: ValidatorEventDetails{
				OwnerPubkey:   ownerPubkey,
				DZIP:          dzIP,
				VotePubkey:    votePubkey,
				NodePubkey:    nodePubkey,
				StakeLamports: stakeLamports,
				StakeSol:      stakeSol,
				StakeSharePct: stakeSharePct,
				UserPK:        pk,
				DevicePK:      devicePK,
				DeviceCode:    deviceCode,
				MetroCode:     metroCode,
				Kind:          validatorKind,
				Action:        action,
			},
		})
	}

	return events, nil
}

// queryGossipNetworkChanges detects when gossip nodes appear or disappear from the Solana network
// This is separate from DZ user status - it tracks the Solana gossip network itself
func queryGossipNetworkChanges(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	// Find gossip nodes that disappeared from the network
	// by tracking node PUBKEYS (not IPs) that are no longer in the current gossip table
	// This correctly handles validators that change IP addresses
	query := `
		WITH total_stake AS (
			SELECT sum(activated_stake_lamports) as total
			FROM solana_vote_accounts_current
		),
		-- Current node pubkeys (nodes that are still online)
		current_pubkeys AS (
			SELECT DISTINCT pubkey FROM solana_gossip_nodes_current
		),
		-- Find nodes (by pubkey) that were seen in history but are no longer in current
		-- Their "offline" time is their last seen timestamp
		disappeared AS (
			SELECT
				gn.pubkey,
				argMax(gn.gossip_ip, gn.snapshot_ts) as last_gossip_ip,
				max(gn.snapshot_ts) as last_seen_ts
			FROM dim_solana_gossip_nodes_history gn
			WHERE gn.pubkey NOT IN (SELECT pubkey FROM current_pubkeys)
			GROUP BY gn.pubkey
			HAVING max(gn.snapshot_ts) >= ? AND max(gn.snapshot_ts) <= ?
		)
		SELECT
			d.last_gossip_ip as gossip_ip,
			d.pubkey as node_pubkey,
			d.last_seen_ts as event_ts,
			'offline' as change_type,
			COALESCE(va_hist.vote_pubkey, '') as vote_pubkey,
			COALESCE(va_hist.stake_lamports, 0) as stake_lamports,
			COALESCE(va_hist.stake_lamports * 100.0 / NULLIF(ts.total, 0), 0) as stake_share_pct,
			COALESCE(u.owner_pubkey, '') as dz_owner_pubkey,
			COALESCE(u.pk, '') as user_pk,
			COALESCE(dev.code, '') as device_code,
			COALESCE(dev.pk, '') as device_pk,
			COALESCE(m.code, '') as metro_code
		FROM disappeared d
		CROSS JOIN total_stake ts
		-- Get historical vote account info (since node is offline, won't be in current)
		LEFT JOIN (
			SELECT node_pubkey, argMax(vote_pubkey, snapshot_ts) as vote_pubkey,
			       argMax(activated_stake_lamports, snapshot_ts) as stake_lamports
			FROM dim_solana_vote_accounts_history
			GROUP BY node_pubkey
		) va_hist ON d.pubkey = va_hist.node_pubkey
		-- Check if this node was connected to DZ (using last known IP)
		LEFT JOIN dz_users_current u ON d.last_gossip_ip = u.dz_ip
		LEFT JOIN dz_devices_current dev ON u.device_pk = dev.pk
		LEFT JOIN dz_metros_current m ON dev.metro_pk = m.pk
		ORDER BY d.last_seen_ts DESC
		LIMIT 100
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			gossipIP      string
			nodePubkey    string
			eventTS       time.Time
			changeType    string
			votePubkey    string
			stakeLamports int64
			stakeSharePct float64
			dzOwnerPubkey string
			userPK        string
			deviceCode    string
			devicePK      string
			metroCode     string
		)

		if err := rows.Scan(&gossipIP, &nodePubkey, &eventTS, &changeType, &votePubkey, &stakeLamports, &stakeSharePct, &dzOwnerPubkey, &userPK, &deviceCode, &devicePK, &metroCode); err != nil {
			return nil, fmt.Errorf("gossip network change scan error: %w", err)
		}

		stakeSol := float64(stakeLamports) / 1_000_000_000
		isValidator := votePubkey != ""

		var title string
		var eventType string
		var entityType string
		var severity string

		if changeType == "offline" {
			if isValidator {
				if stakeSol > 0 {
					title = fmt.Sprintf("Validator left Solana network (%.0f SOL, %.2f%%)", stakeSol, stakeSharePct)
				} else {
					title = "Validator left Solana network"
				}
				eventType = "validator_offline"
				entityType = "validator"
			} else {
				title = "Gossip node left Solana network"
				eventType = "gossip_node_offline"
				entityType = "gossip_node"
			}
			severity = "warning"
		}

		// Use node pubkey as entity code since that's the Solana identity
		entityCode := nodePubkey
		entityPK := nodePubkey

		events = append(events, TimelineEvent{
			ID:          generateEventID(nodePubkey, eventTS, eventType),
			EventType:   eventType,
			Timestamp:   eventTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    severity,
			Title:       title,
			Description: "",
			EntityType:  entityType,
			EntityPK:    entityPK,
			EntityCode:  entityCode,
			Details: ValidatorEventDetails{
				OwnerPubkey:   dzOwnerPubkey,
				DZIP:          gossipIP,
				VotePubkey:    votePubkey,
				NodePubkey:    nodePubkey,
				StakeLamports: stakeLamports,
				StakeSol:      stakeSol,
				StakeSharePct: stakeSharePct,
				UserPK:        userPK,
				DevicePK:      devicePK,
				DeviceCode:    deviceCode,
				MetroCode:     metroCode,
				Kind:          map[bool]string{true: "validator", false: "gossip_only"}[isValidator],
				Action:        "offline",
			},
		})
	}

	return events, nil
}

func queryVoteAccountChanges(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	// Track validators (vote accounts) joining or leaving the network
	// A validator "joins" when their vote_pubkey first appears in the vote accounts table
	// A validator "leaves" when their vote_pubkey is no longer in the current vote accounts table
	query := `
		WITH total_stake AS (
			SELECT sum(activated_stake_lamports) as total
			FROM solana_vote_accounts_current
		),
		-- Current vote pubkeys (validators that are currently active)
		current_vote_pubkeys AS (
			SELECT DISTINCT vote_pubkey FROM solana_vote_accounts_current
		),
		-- Find validators that left: were in history but not in current
		-- Their "left" time is their last seen timestamp
		left_validators AS (
			SELECT
				va.vote_pubkey,
				argMax(va.node_pubkey, va.snapshot_ts) as node_pubkey,
				argMax(va.activated_stake_lamports, va.snapshot_ts) as last_stake,
				max(va.snapshot_ts) as last_seen_ts
			FROM dim_solana_vote_accounts_history va
			WHERE va.vote_pubkey NOT IN (SELECT vote_pubkey FROM current_vote_pubkeys)
			  AND va.activated_stake_lamports > 0
			GROUP BY va.vote_pubkey
			HAVING max(va.snapshot_ts) >= ? AND max(va.snapshot_ts) <= ?
		),
		-- Find validators that joined: first appeared within the time range
		joined_validators AS (
			SELECT
				va.vote_pubkey,
				argMin(va.node_pubkey, va.snapshot_ts) as node_pubkey,
				argMin(va.activated_stake_lamports, va.snapshot_ts) as first_stake,
				min(va.snapshot_ts) as first_seen_ts
			FROM dim_solana_vote_accounts_history va
			WHERE va.activated_stake_lamports > 0
			GROUP BY va.vote_pubkey
			HAVING min(va.snapshot_ts) >= ? AND min(va.snapshot_ts) <= ?
		)
		SELECT
			lv.vote_pubkey,
			lv.node_pubkey,
			lv.last_seen_ts as event_ts,
			'left' as change_type,
			lv.last_stake as stake_lamports,
			lv.last_stake * 100.0 / NULLIF(ts.total, 0) as stake_share_pct,
			COALESCE(gn.gossip_ip, '') as gossip_ip,
			COALESCE(u.owner_pubkey, '') as dz_owner_pubkey,
			COALESCE(u.pk, '') as user_pk,
			COALESCE(dev.code, '') as device_code,
			COALESCE(dev.pk, '') as device_pk,
			COALESCE(m.code, '') as metro_code
		FROM left_validators lv
		CROSS JOIN total_stake ts
		LEFT JOIN solana_gossip_nodes_current gn ON lv.node_pubkey = gn.pubkey
		LEFT JOIN dz_users_current u ON gn.gossip_ip = u.dz_ip
		LEFT JOIN dz_devices_current dev ON u.device_pk = dev.pk
		LEFT JOIN dz_metros_current m ON dev.metro_pk = m.pk

		UNION ALL

		SELECT
			jv.vote_pubkey,
			jv.node_pubkey,
			jv.first_seen_ts as event_ts,
			'joined' as change_type,
			jv.first_stake as stake_lamports,
			jv.first_stake * 100.0 / NULLIF(ts.total, 0) as stake_share_pct,
			COALESCE(gn.gossip_ip, '') as gossip_ip,
			COALESCE(u.owner_pubkey, '') as dz_owner_pubkey,
			COALESCE(u.pk, '') as user_pk,
			COALESCE(dev.code, '') as device_code,
			COALESCE(dev.pk, '') as device_pk,
			COALESCE(m.code, '') as metro_code
		FROM joined_validators jv
		CROSS JOIN total_stake ts
		LEFT JOIN solana_gossip_nodes_current gn ON jv.node_pubkey = gn.pubkey
		LEFT JOIN dz_users_current u ON gn.gossip_ip = u.dz_ip
		LEFT JOIN dz_devices_current dev ON u.device_pk = dev.pk
		LEFT JOIN dz_metros_current m ON dev.metro_pk = m.pk

		ORDER BY event_ts DESC
		LIMIT 100
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			votePubkey    string
			nodePubkey    string
			eventTS       time.Time
			changeType    string
			stakeLamports int64
			stakeSharePct float64
			gossipIP      string
			dzOwnerPubkey string
			userPK        string
			deviceCode    string
			devicePK      string
			metroCode     string
		)

		if err := rows.Scan(&votePubkey, &nodePubkey, &eventTS, &changeType, &stakeLamports, &stakeSharePct, &gossipIP, &dzOwnerPubkey, &userPK, &deviceCode, &devicePK, &metroCode); err != nil {
			return nil, fmt.Errorf("vote account change scan error: %w", err)
		}

		stakeSol := float64(stakeLamports) / 1_000_000_000

		var title string
		var eventType string
		var severity string

		if changeType == "left" {
			if stakeSol > 0 {
				title = fmt.Sprintf("Validator left Solana network (%.0f SOL, %.2f%%)", stakeSol, stakeSharePct)
			} else {
				title = "Validator left Solana network"
			}
			eventType = "validator_left"
			severity = "warning"
		} else {
			if stakeSol > 0 {
				title = fmt.Sprintf("Validator joined Solana network (%.0f SOL, %.2f%%)", stakeSol, stakeSharePct)
			} else {
				title = "Validator joined Solana network"
			}
			eventType = "validator_joined"
			severity = "info"
		}

		events = append(events, TimelineEvent{
			ID:          generateEventID(votePubkey, eventTS, eventType),
			EventType:   eventType,
			Timestamp:   eventTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    severity,
			Title:       title,
			Description: "",
			EntityType:  "validator",
			EntityPK:    votePubkey,
			EntityCode:  votePubkey,
			Details: ValidatorEventDetails{
				OwnerPubkey:   dzOwnerPubkey,
				DZIP:          gossipIP,
				VotePubkey:    votePubkey,
				NodePubkey:    nodePubkey,
				StakeLamports: stakeLamports,
				StakeSol:      stakeSol,
				StakeSharePct: stakeSharePct,
				UserPK:        userPK,
				DevicePK:      devicePK,
				DeviceCode:    deviceCode,
				MetroCode:     metroCode,
				Kind:          "validator",
				Action:        changeType,
			},
		})
	}

	return events, nil
}

func queryStakeChanges(ctx context.Context, startTime, endTime time.Time) ([]TimelineEvent, error) {
	// Track significant stake changes for validators
	// A significant change is >10k SOL or >5% change
	query := `
		WITH total_stake AS (
			SELECT sum(activated_stake_lamports) as total
			FROM solana_vote_accounts_current
		),
		dz_ips AS (
			SELECT DISTINCT dz_ip FROM dz_users_current WHERE dz_ip != ''
		),
		-- Get stake snapshots within the time range
		stake_snapshots AS (
			SELECT
				va.vote_pubkey,
				va.node_pubkey,
				va.snapshot_ts,
				va.activated_stake_lamports as stake,
				lagInFrame(va.activated_stake_lamports) OVER (PARTITION BY va.vote_pubkey ORDER BY va.snapshot_ts) as prev_stake
			FROM dim_solana_vote_accounts_history va
			WHERE va.snapshot_ts >= ? AND va.snapshot_ts <= ?
			  AND va.activated_stake_lamports > 0
		),
		-- Find significant changes
		significant_changes AS (
			SELECT
				vote_pubkey,
				node_pubkey,
				snapshot_ts,
				stake,
				prev_stake,
				stake - prev_stake as change,
				CASE WHEN prev_stake > 0 THEN (stake - prev_stake) * 100.0 / prev_stake ELSE 0 END as change_pct
			FROM stake_snapshots
			WHERE prev_stake IS NOT NULL
			  AND prev_stake > 0
			  AND (
			  	abs(stake - prev_stake) >= 10000000000000  -- >10k SOL in lamports
			  	OR abs((stake - prev_stake) * 100.0 / prev_stake) >= 5  -- >5% change
			  )
		)
		SELECT
			sc.vote_pubkey,
			sc.node_pubkey,
			sc.snapshot_ts as event_ts,
			sc.stake as current_stake,
			sc.prev_stake,
			sc.change,
			sc.change_pct,
			sc.stake * 100.0 / NULLIF(ts.total, 0) as stake_share_pct,
			COALESCE(gn.gossip_ip, '') as gossip_ip,
			gn.gossip_ip IN (SELECT dz_ip FROM dz_ips) as is_on_dz,
			COALESCE(u.owner_pubkey, '') as dz_owner_pubkey,
			COALESCE(u.pk, '') as user_pk,
			COALESCE(dev.code, '') as device_code,
			COALESCE(dev.pk, '') as device_pk,
			COALESCE(m.code, '') as metro_code
		FROM significant_changes sc
		CROSS JOIN total_stake ts
		LEFT JOIN solana_gossip_nodes_current gn ON sc.node_pubkey = gn.pubkey
		LEFT JOIN dz_users_current u ON gn.gossip_ip = u.dz_ip
		LEFT JOIN dz_devices_current dev ON u.device_pk = dev.pk
		LEFT JOIN dz_metros_current m ON dev.metro_pk = m.pk
		ORDER BY sc.snapshot_ts DESC
		LIMIT 200
	`

	start := time.Now()
	rows, err := config.DB.Query(ctx, query, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	metrics.RecordClickHouseQuery(time.Since(start), err)

	var events []TimelineEvent
	for rows.Next() {
		var (
			votePubkey    string
			nodePubkey    string
			eventTS       time.Time
			currentStake  int64
			prevStake     int64
			change        int64
			changePct     float64
			stakeSharePct float64
			gossipIP      string
			isOnDZ        bool
			dzOwnerPubkey string
			userPK        string
			deviceCode    string
			devicePK      string
			metroCode     string
		)

		if err := rows.Scan(&votePubkey, &nodePubkey, &eventTS, &currentStake, &prevStake, &change, &changePct, &stakeSharePct, &gossipIP, &isOnDZ, &dzOwnerPubkey, &userPK, &deviceCode, &devicePK, &metroCode); err != nil {
			return nil, fmt.Errorf("stake change scan error: %w", err)
		}

		changeSol := float64(change) / 1_000_000_000
		currentSol := float64(currentStake) / 1_000_000_000

		var title string
		var eventType string
		var severity string
		var action string

		// Prefix with DZ status
		dzPrefix := ""
		if isOnDZ {
			dzPrefix = "DZ "
		}

		if change > 0 {
			title = fmt.Sprintf("%sValidator stake increased by %.0f SOL (%.1f%%)", dzPrefix, changeSol, changePct)
			eventType = "stake_increased"
			severity = "info"
			action = "increased"
		} else {
			title = fmt.Sprintf("%sValidator stake decreased by %.0f SOL (%.1f%%)", dzPrefix, -changeSol, -changePct)
			eventType = "stake_decreased"
			severity = "warning"
			action = "decreased"
		}

		// Add context about current stake
		title += fmt.Sprintf(" → %.0f SOL", currentSol)

		events = append(events, TimelineEvent{
			ID:          generateEventID(votePubkey, eventTS, eventType),
			EventType:   eventType,
			Timestamp:   eventTS.Format(time.RFC3339),
			Category:    "state_change",
			Severity:    severity,
			Title:       title,
			Description: "",
			EntityType:  "validator",
			EntityPK:    votePubkey,
			EntityCode:  votePubkey,
			Details: ValidatorEventDetails{
				OwnerPubkey:   dzOwnerPubkey,
				DZIP:          gossipIP,
				VotePubkey:    votePubkey,
				NodePubkey:    nodePubkey,
				StakeLamports: currentStake,
				StakeSol:      currentSol,
				StakeSharePct: stakeSharePct,
				UserPK:        userPK,
				DevicePK:      devicePK,
				DeviceCode:    deviceCode,
				MetroCode:     metroCode,
				Kind:          "validator",
				Action:        action,
			},
		})
	}

	return events, nil
}

// groupInterfaceEvents groups interface events by device, event type, and timestamp.
// When multiple interfaces on the same device have the same issue at the same time,
// they are consolidated into a single event with GroupedInterfaceDetails.
func groupInterfaceEvents(events []TimelineEvent) []TimelineEvent {
	// Key: devicePK + eventType + timestamp
	type groupKey struct {
		DevicePK  string
		EventType string
		Timestamp string
	}

	// Map to collect events by group key
	groups := make(map[groupKey][]TimelineEvent)
	var nonInterfaceEvents []TimelineEvent

	for _, event := range events {
		// Only group interface events
		if details, ok := event.Details.(InterfaceEventDetails); ok {
			key := groupKey{
				DevicePK:  details.DevicePK,
				EventType: event.EventType,
				Timestamp: event.Timestamp,
			}
			groups[key] = append(groups[key], event)
		} else {
			nonInterfaceEvents = append(nonInterfaceEvents, event)
		}
	}

	// Build result with grouped events
	result := nonInterfaceEvents

	for key, groupEvents := range groups {
		if len(groupEvents) == 1 {
			// Single interface, keep as-is
			result = append(result, groupEvents[0])
		} else {
			// Multiple interfaces, create grouped event
			first := groupEvents[0]
			firstDetails := first.Details.(InterfaceEventDetails)

			// Collect all interface details
			interfaces := make([]InterfaceEventDetails, 0, len(groupEvents))
			for _, e := range groupEvents {
				interfaces = append(interfaces, e.Details.(InterfaceEventDetails))
			}

			// Sort interfaces by name for consistent display
			sort.Slice(interfaces, func(i, j int) bool {
				return interfaces[i].InterfaceName < interfaces[j].InterfaceName
			})

			// Build title based on event type
			var title string
			switch key.EventType {
			case "interface_carrier_started":
				title = fmt.Sprintf("Carrier transitions started on %d interfaces on %s", len(interfaces), firstDetails.DeviceCode)
			case "interface_carrier_stopped":
				title = fmt.Sprintf("Carrier transitions stopped on %d interfaces on %s", len(interfaces), firstDetails.DeviceCode)
			case "interface_errors_started":
				title = fmt.Sprintf("Interface errors started on %d interfaces on %s", len(interfaces), firstDetails.DeviceCode)
			case "interface_errors_stopped":
				title = fmt.Sprintf("Interface errors stopped on %d interfaces on %s", len(interfaces), firstDetails.DeviceCode)
			case "interface_discards_started":
				title = fmt.Sprintf("Interface discards started on %d interfaces on %s", len(interfaces), firstDetails.DeviceCode)
			case "interface_discards_stopped":
				title = fmt.Sprintf("Interface discards stopped on %d interfaces on %s", len(interfaces), firstDetails.DeviceCode)
			default:
				title = fmt.Sprintf("%d interface events on %s", len(interfaces), firstDetails.DeviceCode)
			}

			result = append(result, TimelineEvent{
				ID:         generateEventID(firstDetails.DevicePK, mustParseTime(first.Timestamp), key.EventType+"_grouped"),
				EventType:  key.EventType,
				Timestamp:  first.Timestamp,
				Category:   first.Category,
				Severity:   first.Severity,
				Title:      title,
				EntityType: "device",
				EntityPK:   firstDetails.DevicePK,
				EntityCode: firstDetails.DeviceCode,
				Details: GroupedInterfaceDetails{
					DevicePK:   firstDetails.DevicePK,
					DeviceCode: firstDetails.DeviceCode,
					IssueType:  firstDetails.IssueType,
					Interfaces: interfaces,
				},
			})
		}
	}

	return result
}

// mustParseTime parses an RFC3339 timestamp or returns zero time on error
func mustParseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// fetchDefaultTimelineData fetches timeline data with default parameters for caching.
// This returns the same data as GetTimeline with default params: 24h range, all categories,
// all entity types except gossip_node, dz_filter="on_dz", limit=50, offset=0.
func fetchDefaultTimelineData(ctx context.Context) *TimelineResponse {
	now := time.Now().UTC()
	startTime := now.Add(-24 * time.Hour)
	endTime := now

	// Default entity types (all except gossip_node)
	defaultEntityTypes := []string{"device", "link", "metro", "contributor", "user", "validator"}
	dzFilter := "on_dz"
	limit := 50
	offset := 0

	g, ctx := errgroup.WithContext(ctx)
	var (
		deviceEvents        []TimelineEvent
		linkEvents          []TimelineEvent
		metroEvents         []TimelineEvent
		contributorEvents   []TimelineEvent
		userEvents          []TimelineEvent
		packetLossEvents    []TimelineEvent
		interfaceEvents     []TimelineEvent
		validatorEvents     []TimelineEvent
		gossipNetworkEvents []TimelineEvent
		voteAccountEvents   []TimelineEvent
		stakeChangeEvents   []TimelineEvent
		mu                  sync.Mutex
	)

	// Query all event types (all categories included by default)

	// Device changes
	g.Go(func() error {
		events, err := queryDeviceChanges(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying device changes: %v", err)
			return nil
		}
		mu.Lock()
		deviceEvents = events
		mu.Unlock()
		return nil
	})

	// Link changes
	g.Go(func() error {
		events, err := queryLinkChanges(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying link changes: %v", err)
			return nil
		}
		mu.Lock()
		linkEvents = events
		mu.Unlock()
		return nil
	})

	// Metro changes
	g.Go(func() error {
		events, err := queryMetroChanges(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying metro changes: %v", err)
			return nil
		}
		mu.Lock()
		metroEvents = events
		mu.Unlock()
		return nil
	})

	// Contributor changes
	g.Go(func() error {
		events, err := queryContributorChanges(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying contributor changes: %v", err)
			return nil
		}
		mu.Lock()
		contributorEvents = events
		mu.Unlock()
		return nil
	})

	// User changes (exclude internal users by default)
	g.Go(func() error {
		events, err := queryUserChanges(ctx, startTime, endTime, false)
		if err != nil {
			log.Printf("Cache: Error querying user changes: %v", err)
			return nil
		}
		mu.Lock()
		userEvents = events
		mu.Unlock()
		return nil
	})

	// Packet loss events
	g.Go(func() error {
		events, err := queryPacketLossEvents(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying packet loss events: %v", err)
			return nil
		}
		mu.Lock()
		packetLossEvents = events
		mu.Unlock()
		return nil
	})

	// Interface events
	g.Go(func() error {
		events, err := queryInterfaceEvents(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying interface events: %v", err)
			return nil
		}
		mu.Lock()
		interfaceEvents = events
		mu.Unlock()
		return nil
	})

	// Validator events (exclude internal users by default)
	g.Go(func() error {
		events, err := queryValidatorEvents(ctx, startTime, endTime, false)
		if err != nil {
			log.Printf("Cache: Error querying validator events: %v", err)
			return nil
		}
		mu.Lock()
		validatorEvents = events
		mu.Unlock()
		return nil
	})

	// Gossip network changes
	g.Go(func() error {
		events, err := queryGossipNetworkChanges(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying gossip network changes: %v", err)
			return nil
		}
		mu.Lock()
		gossipNetworkEvents = events
		mu.Unlock()
		return nil
	})

	// Vote account changes
	g.Go(func() error {
		events, err := queryVoteAccountChanges(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying vote account changes: %v", err)
			return nil
		}
		mu.Lock()
		voteAccountEvents = events
		mu.Unlock()
		return nil
	})

	// Stake changes
	g.Go(func() error {
		events, err := queryStakeChanges(ctx, startTime, endTime)
		if err != nil {
			log.Printf("Cache: Error querying stake changes: %v", err)
			return nil
		}
		mu.Lock()
		stakeChangeEvents = events
		mu.Unlock()
		return nil
	})

	if err := g.Wait(); err != nil {
		log.Printf("Cache: Error in timeline queries: %v", err)
	}

	// Merge all events
	allEvents := make([]TimelineEvent, 0)
	allEvents = append(allEvents, deviceEvents...)
	allEvents = append(allEvents, linkEvents...)
	allEvents = append(allEvents, metroEvents...)
	allEvents = append(allEvents, contributorEvents...)
	allEvents = append(allEvents, userEvents...)
	allEvents = append(allEvents, packetLossEvents...)
	allEvents = append(allEvents, interfaceEvents...)
	allEvents = append(allEvents, validatorEvents...)
	allEvents = append(allEvents, gossipNetworkEvents...)
	allEvents = append(allEvents, voteAccountEvents...)
	allEvents = append(allEvents, stakeChangeEvents...)

	// Group interface events
	allEvents = groupInterfaceEvents(allEvents)

	// Filter by default entity types (exclude gossip_node)
	filtered := make([]TimelineEvent, 0)
	entityTypeSet := make(map[string]bool)
	for _, et := range defaultEntityTypes {
		entityTypeSet[et] = true
	}
	for _, e := range allEvents {
		if entityTypeSet[e.EntityType] {
			filtered = append(filtered, e)
		}
	}
	allEvents = filtered

	// Filter Solana events by DZ connection status (on_dz by default)
	if dzFilter == "on_dz" {
		filtered := make([]TimelineEvent, 0)
		for _, e := range allEvents {
			if e.EntityType == "validator" || e.EntityType == "gossip_node" {
				if details, ok := e.Details.(ValidatorEventDetails); ok {
					isOnDZ := details.OwnerPubkey != "" || details.DevicePK != ""
					if isOnDZ {
						filtered = append(filtered, e)
					}
				} else {
					isOnDZ := strings.HasPrefix(e.Title, "DZ ")
					if isOnDZ {
						filtered = append(filtered, e)
					}
				}
			} else {
				filtered = append(filtered, e)
			}
		}
		allEvents = filtered
	}

	// Sort by timestamp descending
	sort.Slice(allEvents, func(i, j int) bool {
		if allEvents[i].Timestamp != allEvents[j].Timestamp {
			return allEvents[i].Timestamp > allEvents[j].Timestamp
		}
		return allEvents[i].ID > allEvents[j].ID
	})

	total := len(allEvents)

	// Apply pagination
	startIdx := offset
	endIdx := offset + limit
	if startIdx > len(allEvents) {
		startIdx = len(allEvents)
	}
	if endIdx > len(allEvents) {
		endIdx = len(allEvents)
	}
	paginatedEvents := allEvents[startIdx:endIdx]

	// Compute histogram
	histogram := computeHistogram(allEvents, startTime, endTime)

	return &TimelineResponse{
		Events: paginatedEvents,
		Total:  total,
		Limit:  limit,
		Offset: offset,
		TimeRange: TimeRange{
			Start: startTime.Format(time.RFC3339),
			End:   endTime.Format(time.RFC3339),
		},
		Histogram: histogram,
	}
}
