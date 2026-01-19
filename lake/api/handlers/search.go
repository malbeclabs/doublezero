package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/api/metrics"
	"golang.org/x/sync/errgroup"
)

// SearchSuggestion represents a single autocomplete suggestion
type SearchSuggestion struct {
	Type     string `json:"type"`
	ID       string `json:"id"`
	Label    string `json:"label"`
	Sublabel string `json:"sublabel"`
	URL      string `json:"url"`
}

// AutocompleteResponse is the response for the autocomplete endpoint
type AutocompleteResponse struct {
	Suggestions []SearchSuggestion `json:"suggestions"`
}

// SearchResultGroup represents search results for a single entity type
type SearchResultGroup struct {
	Items []SearchSuggestion `json:"items"`
	Total int                `json:"total"`
}

// SearchResponse is the response for the full search endpoint
type SearchResponse struct {
	Query   string                       `json:"query"`
	Results map[string]SearchResultGroup `json:"results"`
}

// entityType defines which entity types to search
type entityType string

const (
	entityDevice      entityType = "device"
	entityLink        entityType = "link"
	entityMetro       entityType = "metro"
	entityContributor entityType = "contributor"
	entityUser        entityType = "user"
	entityValidator   entityType = "validator"
	entityGossip      entityType = "gossip"
)

var allEntityTypes = []entityType{
	entityDevice,
	entityLink,
	entityMetro,
	entityContributor,
	entityUser,
	entityValidator,
	entityGossip,
}

// fieldPrefix maps search prefixes to entity types
var fieldPrefixes = map[string][]entityType{
	"device:":      {entityDevice},
	"link:":        {entityLink},
	"metro:":       {entityMetro},
	"contributor:": {entityContributor},
	"user:":        {entityUser},
	"validator:":   {entityValidator},
	"gossip:":      {entityGossip},
	"ip:":          {entityDevice, entityUser, entityGossip},
	"pubkey:":      {entityUser, entityValidator, entityGossip},
}

// parseQuery extracts the search term and target entity types from a query
func parseQuery(q string) (term string, types []entityType) {
	q = strings.TrimSpace(q)
	for prefix, entityTypes := range fieldPrefixes {
		if strings.HasPrefix(strings.ToLower(q), prefix) {
			return strings.TrimSpace(q[len(prefix):]), entityTypes
		}
	}
	return q, allEntityTypes
}

// buildSearchCondition builds a WHERE clause for multi-token search.
// Each token must match at least one of the fields.
// Returns the condition string and the arguments.
func buildSearchCondition(term string, fields []string) (string, []any) {
	tokens := strings.Fields(term)
	if len(tokens) == 0 {
		return "1=0", nil
	}

	var args []any
	var tokenConditions []string

	for _, token := range tokens {
		pattern := "%" + token + "%"
		var fieldConditions []string
		for _, field := range fields {
			fieldConditions = append(fieldConditions, fmt.Sprintf("%s ILIKE ?", field))
			args = append(args, pattern)
		}
		tokenConditions = append(tokenConditions, "("+strings.Join(fieldConditions, " OR ")+")")
	}

	return strings.Join(tokenConditions, " AND "), args
}

// searchDevices searches for devices matching the query
func searchDevices(ctx context.Context, term string, limit int) ([]SearchSuggestion, int, error) {
	// Count query uses unqualified names (single table)
	countFields := []string{"code", "pk", "public_ip"}
	countCondition, countArgs := buildSearchCondition(term, countFields)

	countQuery := `SELECT count(*) FROM dz_devices_current WHERE ` + countCondition
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Main query uses qualified names (has JOIN)
	mainFields := []string{"d.code", "d.pk", "d.public_ip"}
	mainCondition, mainArgs := buildSearchCondition(term, mainFields)

	query := `
		SELECT
			d.pk,
			d.code,
			d.device_type,
			COALESCE(m.code, '') as metro_code
		FROM dz_devices_current d
		LEFT JOIN dz_metros_current m ON d.metro_pk = m.pk
		WHERE ` + mainCondition + `
		ORDER BY d.code
		LIMIT ?
	`

	rows, err := config.DB.Query(ctx, query, append(mainArgs, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var suggestions []SearchSuggestion
	for rows.Next() {
		var pk, code, deviceType, metroCode string
		if err := rows.Scan(&pk, &code, &deviceType, &metroCode); err != nil {
			return nil, 0, err
		}
		sublabel := deviceType
		if metroCode != "" {
			sublabel = fmt.Sprintf("%s - %s", deviceType, metroCode)
		}
		suggestions = append(suggestions, SearchSuggestion{
			Type:     string(entityDevice),
			ID:       pk,
			Label:    code,
			Sublabel: sublabel,
			URL:      fmt.Sprintf("/dz/devices/%s", pk),
		})
	}
	return suggestions, int(total), nil
}

// searchLinks searches for links matching the query
func searchLinks(ctx context.Context, term string, limit int) ([]SearchSuggestion, int, error) {
	// Count query uses unqualified names (single table)
	countFields := []string{"code", "pk"}
	countCondition, countArgs := buildSearchCondition(term, countFields)

	countQuery := `SELECT count(*) FROM dz_links_current WHERE ` + countCondition
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Main query uses qualified names (has JOINs)
	mainFields := []string{"l.code", "l.pk"}
	mainCondition, mainArgs := buildSearchCondition(term, mainFields)

	query := `
		SELECT
			l.pk,
			l.code,
			COALESCE(ma.code, '') as side_a_metro,
			COALESCE(mz.code, '') as side_z_metro
		FROM dz_links_current l
		LEFT JOIN dz_devices_current da ON l.side_a_pk = da.pk
		LEFT JOIN dz_metros_current ma ON da.metro_pk = ma.pk
		LEFT JOIN dz_devices_current dz ON l.side_z_pk = dz.pk
		LEFT JOIN dz_metros_current mz ON dz.metro_pk = mz.pk
		WHERE ` + mainCondition + `
		ORDER BY l.code
		LIMIT ?
	`

	rows, err := config.DB.Query(ctx, query, append(mainArgs, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var suggestions []SearchSuggestion
	for rows.Next() {
		var pk, code, sideAMetro, sideZMetro string
		if err := rows.Scan(&pk, &code, &sideAMetro, &sideZMetro); err != nil {
			return nil, 0, err
		}
		sublabel := "Link"
		if sideAMetro != "" && sideZMetro != "" {
			sublabel = fmt.Sprintf("%s <-> %s", sideAMetro, sideZMetro)
		}
		suggestions = append(suggestions, SearchSuggestion{
			Type:     string(entityLink),
			ID:       pk,
			Label:    code,
			Sublabel: sublabel,
			URL:      fmt.Sprintf("/dz/links/%s", pk),
		})
	}
	return suggestions, int(total), nil
}

// searchMetros searches for metros matching the query
func searchMetros(ctx context.Context, term string, limit int) ([]SearchSuggestion, int, error) {
	fields := []string{"code", "name", "pk"}
	condition, args := buildSearchCondition(term, fields)

	countQuery := `SELECT count(*) FROM dz_metros_current WHERE ` + condition
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT pk, code, name
		FROM dz_metros_current
		WHERE ` + condition + `
		ORDER BY code
		LIMIT ?
	`

	rows, err := config.DB.Query(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var suggestions []SearchSuggestion
	for rows.Next() {
		var pk, code, name string
		if err := rows.Scan(&pk, &code, &name); err != nil {
			return nil, 0, err
		}
		suggestions = append(suggestions, SearchSuggestion{
			Type:     string(entityMetro),
			ID:       pk,
			Label:    code,
			Sublabel: name,
			URL:      fmt.Sprintf("/dz/metros/%s", pk),
		})
	}
	return suggestions, int(total), nil
}

// searchContributors searches for contributors matching the query
func searchContributors(ctx context.Context, term string, limit int) ([]SearchSuggestion, int, error) {
	fields := []string{"code", "name", "pk"}
	condition, args := buildSearchCondition(term, fields)

	countQuery := `SELECT count(*) FROM dz_contributors_current WHERE ` + condition
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	query := `
		SELECT pk, code, name
		FROM dz_contributors_current
		WHERE ` + condition + `
		ORDER BY code
		LIMIT ?
	`

	rows, err := config.DB.Query(ctx, query, append(args, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var suggestions []SearchSuggestion
	for rows.Next() {
		var pk, code, name string
		if err := rows.Scan(&pk, &code, &name); err != nil {
			return nil, 0, err
		}
		suggestions = append(suggestions, SearchSuggestion{
			Type:     string(entityContributor),
			ID:       pk,
			Label:    code,
			Sublabel: name,
			URL:      fmt.Sprintf("/dz/contributors/%s", pk),
		})
	}
	return suggestions, int(total), nil
}

// searchUsers searches for users matching the query
func searchUsers(ctx context.Context, term string, limit int) ([]SearchSuggestion, int, error) {
	// Count query uses unqualified names
	countFields := []string{"pk", "owner_pubkey", "dz_ip"}
	countCondition, countArgs := buildSearchCondition(term, countFields)

	countQuery := `SELECT count(*) FROM dz_users_current WHERE ` + countCondition
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Main query uses qualified names
	mainFields := []string{"u.pk", "u.owner_pubkey", "u.dz_ip"}
	mainCondition, mainArgs := buildSearchCondition(term, mainFields)

	query := `
		SELECT
			u.pk,
			u.kind,
			COALESCE(u.dz_ip, '') as dz_ip
		FROM dz_users_current u
		WHERE ` + mainCondition + `
		ORDER BY u.pk
		LIMIT ?
	`

	rows, err := config.DB.Query(ctx, query, append(mainArgs, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var suggestions []SearchSuggestion
	for rows.Next() {
		var pk, kind, dzIP string
		if err := rows.Scan(&pk, &kind, &dzIP); err != nil {
			return nil, 0, err
		}
		// Truncate pk for display
		label := pk
		if len(pk) > 12 {
			label = pk[:8] + "..."
		}
		sublabel := kind
		if dzIP != "" {
			sublabel = fmt.Sprintf("%s - %s", kind, dzIP)
		}
		suggestions = append(suggestions, SearchSuggestion{
			Type:     string(entityUser),
			ID:       pk,
			Label:    label,
			Sublabel: sublabel,
			URL:      fmt.Sprintf("/dz/users/%s", pk),
		})
	}
	return suggestions, int(total), nil
}

// searchValidators searches for validators matching the query
func searchValidators(ctx context.Context, term string, limit int) ([]SearchSuggestion, int, error) {
	// Count query uses unqualified names
	countFields := []string{"vote_pubkey", "node_pubkey"}
	countCondition, countArgs := buildSearchCondition(term, countFields)

	countQuery := `
		SELECT count(*)
		FROM solana_vote_accounts_current
		WHERE epoch_vote_account = 'true'
		AND (` + countCondition + `)
	`
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Main query uses qualified names
	mainFields := []string{"v.vote_pubkey", "v.node_pubkey"}
	mainCondition, mainArgs := buildSearchCondition(term, mainFields)

	query := `
		SELECT
			v.vote_pubkey,
			v.node_pubkey,
			v.activated_stake_lamports
		FROM solana_vote_accounts_current v
		WHERE v.epoch_vote_account = 'true'
		AND (` + mainCondition + `)
		ORDER BY v.activated_stake_lamports DESC
		LIMIT ?
	`

	rows, err := config.DB.Query(ctx, query, append(mainArgs, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var suggestions []SearchSuggestion
	for rows.Next() {
		var votePubkey, nodePubkey string
		var stakeLamports int64
		if err := rows.Scan(&votePubkey, &nodePubkey, &stakeLamports); err != nil {
			return nil, 0, err
		}
		// Truncate pubkey for display
		label := votePubkey
		if len(votePubkey) > 12 {
			label = votePubkey[:8] + "..."
		}
		// Format stake
		stakeSOL := float64(stakeLamports) / 1e9
		sublabel := fmt.Sprintf("%.1fK SOL", stakeSOL/1000)
		if stakeSOL >= 1000000 {
			sublabel = fmt.Sprintf("%.2fM SOL", stakeSOL/1000000)
		}
		suggestions = append(suggestions, SearchSuggestion{
			Type:     string(entityValidator),
			ID:       votePubkey,
			Label:    label,
			Sublabel: sublabel,
			URL:      fmt.Sprintf("/solana/validators/%s", votePubkey),
		})
	}
	return suggestions, int(total), nil
}

// searchGossipNodes searches for gossip nodes matching the query
func searchGossipNodes(ctx context.Context, term string, limit int) ([]SearchSuggestion, int, error) {
	// Count query uses unqualified names
	countFields := []string{"pubkey", "gossip_ip"}
	countCondition, countArgs := buildSearchCondition(term, countFields)

	countQuery := `SELECT count(*) FROM solana_gossip_nodes_current WHERE ` + countCondition
	var total uint64
	if err := config.DB.QueryRow(ctx, countQuery, countArgs...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Main query uses qualified names
	mainFields := []string{"g.pubkey", "g.gossip_ip"}
	mainCondition, mainArgs := buildSearchCondition(term, mainFields)

	query := `
		SELECT
			g.pubkey,
			COALESCE(g.version, '') as version,
			COALESCE(g.gossip_ip, '') as gossip_ip
		FROM solana_gossip_nodes_current g
		WHERE ` + mainCondition + `
		ORDER BY g.pubkey
		LIMIT ?
	`

	rows, err := config.DB.Query(ctx, query, append(mainArgs, limit)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var suggestions []SearchSuggestion
	for rows.Next() {
		var pubkey, version, gossipIP string
		if err := rows.Scan(&pubkey, &version, &gossipIP); err != nil {
			return nil, 0, err
		}
		// Truncate pubkey for display
		label := pubkey
		if len(pubkey) > 12 {
			label = pubkey[:8] + "..."
		}
		sublabel := version
		if gossipIP != "" && version != "" {
			sublabel = fmt.Sprintf("%s - %s", version, gossipIP)
		} else if gossipIP != "" {
			sublabel = gossipIP
		}
		suggestions = append(suggestions, SearchSuggestion{
			Type:     string(entityGossip),
			ID:       pubkey,
			Label:    label,
			Sublabel: sublabel,
			URL:      fmt.Sprintf("/solana/gossip-nodes/%s", pubkey),
		})
	}
	return suggestions, int(total), nil
}

// SearchAutocomplete handles the autocomplete endpoint
func SearchAutocomplete(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	start := time.Now()

	q := r.URL.Query().Get("q")
	if len(q) < 2 {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AutocompleteResponse{Suggestions: []SearchSuggestion{}})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 50 {
			limit = l
		}
	}

	term, types := parseQuery(q)
	if term == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(AutocompleteResponse{Suggestions: []SearchSuggestion{}})
		return
	}

	// Limit per entity type (distribute evenly, prioritize validators)
	perTypeLimit := (limit / len(types)) + 1
	if perTypeLimit < 3 {
		perTypeLimit = 3
	}

	// Run searches in parallel
	type searchResult struct {
		entityType  entityType
		suggestions []SearchSuggestion
	}
	resultsChan := make(chan searchResult, len(types))

	g, gCtx := errgroup.WithContext(ctx)
	for _, et := range types {
		et := et
		g.Go(func() error {
			var suggestions []SearchSuggestion
			var err error
			switch et {
			case entityDevice:
				suggestions, _, err = searchDevices(gCtx, term, perTypeLimit)
			case entityLink:
				suggestions, _, err = searchLinks(gCtx, term, perTypeLimit)
			case entityMetro:
				suggestions, _, err = searchMetros(gCtx, term, perTypeLimit)
			case entityContributor:
				suggestions, _, err = searchContributors(gCtx, term, perTypeLimit)
			case entityUser:
				suggestions, _, err = searchUsers(gCtx, term, perTypeLimit)
			case entityValidator:
				suggestions, _, err = searchValidators(gCtx, term, perTypeLimit)
			case entityGossip:
				suggestions, _, err = searchGossipNodes(gCtx, term, perTypeLimit)
			}
			if err != nil {
				log.Printf("Search %s error: %v", et, err)
				return nil // Don't fail the whole search
			}
			resultsChan <- searchResult{entityType: et, suggestions: suggestions}
			return nil
		})
	}

	g.Wait()
	close(resultsChan)

	// Collect results and merge
	var allSuggestions []SearchSuggestion
	for result := range resultsChan {
		allSuggestions = append(allSuggestions, result.suggestions...)
	}

	// Trim to limit
	if len(allSuggestions) > limit {
		allSuggestions = allSuggestions[:limit]
	}

	metrics.RecordClickHouseQuery(time.Since(start), nil)

	w.Header().Set("Content-Type", "application/json")
	if allSuggestions == nil {
		allSuggestions = []SearchSuggestion{}
	}
	json.NewEncoder(w).Encode(AutocompleteResponse{Suggestions: allSuggestions})
}

// Search handles the full search endpoint
func Search(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	start := time.Now()

	q := r.URL.Query().Get("q")
	if q == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SearchResponse{Query: "", Results: map[string]SearchResultGroup{}})
		return
	}

	limitStr := r.URL.Query().Get("limit")
	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	// Parse types filter
	typesStr := r.URL.Query().Get("types")
	term, types := parseQuery(q)
	if typesStr != "" {
		// Override with explicit types
		var filteredTypes []entityType
		for _, t := range strings.Split(typesStr, ",") {
			t = strings.TrimSpace(t)
			for _, et := range allEntityTypes {
				if string(et) == t {
					filteredTypes = append(filteredTypes, et)
					break
				}
			}
		}
		if len(filteredTypes) > 0 {
			types = filteredTypes
		}
	}

	if term == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SearchResponse{Query: q, Results: map[string]SearchResultGroup{}})
		return
	}

	// Run searches in parallel
	type searchResult struct {
		entityType  entityType
		suggestions []SearchSuggestion
		total       int
	}
	resultsChan := make(chan searchResult, len(types))

	g, gCtx := errgroup.WithContext(ctx)
	for _, et := range types {
		et := et
		g.Go(func() error {
			var suggestions []SearchSuggestion
			var total int
			var err error
			switch et {
			case entityDevice:
				suggestions, total, err = searchDevices(gCtx, term, limit)
			case entityLink:
				suggestions, total, err = searchLinks(gCtx, term, limit)
			case entityMetro:
				suggestions, total, err = searchMetros(gCtx, term, limit)
			case entityContributor:
				suggestions, total, err = searchContributors(gCtx, term, limit)
			case entityUser:
				suggestions, total, err = searchUsers(gCtx, term, limit)
			case entityValidator:
				suggestions, total, err = searchValidators(gCtx, term, limit)
			case entityGossip:
				suggestions, total, err = searchGossipNodes(gCtx, term, limit)
			}
			if err != nil {
				log.Printf("Search %s error: %v", et, err)
				return nil // Don't fail the whole search
			}
			resultsChan <- searchResult{entityType: et, suggestions: suggestions, total: total}
			return nil
		})
	}

	g.Wait()
	close(resultsChan)

	// Collect results
	results := make(map[string]SearchResultGroup)
	for result := range resultsChan {
		if len(result.suggestions) > 0 || result.total > 0 {
			results[string(result.entityType)] = SearchResultGroup{
				Items: result.suggestions,
				Total: result.total,
			}
		}
	}

	metrics.RecordClickHouseQuery(time.Since(start), nil)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SearchResponse{Query: q, Results: results})
}
