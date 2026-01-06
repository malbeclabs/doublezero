// Package isis provides ISIS LSP JSON parsing and enrichment for LLM consumption.
//
// The package transforms ISIS Link State Protocol JSON data into structured
// markdown optimized for near 100% query accuracy by pre-computing counts,
// averages, sorted lists, and self-contained router blocks.
package isis

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Router represents parsed router data from an ISIS LSP.
type Router struct {
	Hostname       string
	RouterID       string
	SystemID       string
	RouterType     string
	Area           string
	IsOverloaded   bool
	Sequence       int
	Interfaces     []string
	Neighbors      []Neighbor
	Reachabilities []Reachability
	SRGBBase       *int
	SRGBRange      *int
	SRGBEnd        *int
	SRLBBase       *int
	SRLBRange      *int
	SRLBEnd        *int
	MSD            *int
	NodeSID        *int
	NodeSIDPrefix  string
	Location       string
}

// Neighbor represents an IS-IS adjacency.
type Neighbor struct {
	Hostname     string
	Metric       int
	NeighborAddr string
	AdjSIDs      []int
}

// Reachability represents a prefix advertised by a router.
type Reachability struct {
	Prefix string
	Metric int
	SR     *SRInfo
}

// SRInfo holds Segment Routing information for a prefix.
type SRInfo struct {
	SID       int
	IsNodeSID bool
}

// EnricherConfig holds configuration for the Enricher.
type EnricherConfig struct {
	// Level is the ISIS level to process (1 or 2). Default: 2.
	Level int
}

// Enricher transforms ISIS JSON data into structured markdown.
type Enricher struct {
	locator *Locator
	level   int
}

// NewEnricher creates a new Enricher with the given configuration.
func NewEnricher(cfg EnricherConfig) (*Enricher, error) {
	locator, err := DefaultLocator()
	if err != nil {
		return nil, fmt.Errorf("failed to initialize locator: %w", err)
	}

	level := cfg.Level
	if level == 0 {
		level = 2
	}

	return &Enricher{
		locator: locator,
		level:   level,
	}, nil
}

// Result contains the enrichment output.
type Result struct {
	Markdown string
	Routers  map[string]Router
	Stats    NetworkStats
}

// EnrichFromReader processes JSON from an io.Reader.
func (e *Enricher) EnrichFromReader(_ context.Context, r io.Reader, timestamp string) (*Result, error) {
	lsps, err := parseLSPs(r, e.level)
	if err != nil {
		return nil, err
	}

	routers := make(map[string]Router, len(lsps))
	for lspID, lsp := range lsps {
		router := parseRouterFromLSP(lspID, lsp, e.locator)
		routers[router.Hostname] = router
	}

	stats := computeStats(routers)

	markdown, err := generateMarkdown(routers, stats, timestamp, e.locator)
	if err != nil {
		return nil, fmt.Errorf("failed to generate markdown: %w", err)
	}

	return &Result{
		Markdown: markdown,
		Routers:  routers,
		Stats:    stats,
	}, nil
}

// EnrichFromFile processes a JSON file and returns enriched markdown.
func (e *Enricher) EnrichFromFile(ctx context.Context, path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer f.Close()

	timestamp := extractTimestamp(path)
	return e.EnrichFromReader(ctx, f, timestamp)
}

// FindLatestJSON finds the most recent JSON file in a directory.
// Returns empty string if no matching files found.
func FindLatestJSON(dir string) (string, error) {
	pattern := filepath.Join(dir, "*_upload_data.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	if len(matches) == 0 {
		return "", nil
	}

	// Sort by filename descending (timestamps in filename)
	sort.Sort(sort.Reverse(sort.StringSlice(matches)))
	return matches[0], nil
}

// extractTimestamp extracts a human-readable timestamp from a filename.
// Example: 2026-01-06T15-42-13Z_upload_data.json -> "2026-01-06 15:42:13 UTC"
func extractTimestamp(path string) string {
	name := filepath.Base(path)
	name = strings.TrimSuffix(name, ".json")

	re := regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})T(\d{2})-(\d{2})-(\d{2})Z`)
	match := re.FindStringSubmatch(name)
	if match == nil {
		return name
	}

	return fmt.Sprintf("%s %s:%s:%s UTC", match[1], match[2], match[3], match[4])
}
