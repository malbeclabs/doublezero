package isis

import (
	"bytes"
	_ "embed"
	"sort"
	"strings"
	"text/template"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

//go:embed templates/report.md.tmpl
var reportTemplate string

// NeighborBucket groups routers by neighbor count for display.
type NeighborBucket struct {
	Label   string
	Routers string
}

// LocationBucket groups routers by location for display.
type LocationBucket struct {
	Location    string
	Description string
	Routers     string
}

// templateData holds all data needed for template rendering.
type templateData struct {
	Timestamp       string
	Stats           NetworkStats
	Routers         map[string]Router
	SortedHostnames []string
	NeighborBuckets []NeighborBucket
	LocationBuckets []LocationBucket
}

// newReportTemplate creates a template with custom functions.
func newReportTemplate() (*template.Template, error) {
	printer := message.NewPrinter(language.English)

	funcMap := template.FuncMap{
		"join": strings.Join,
		"formatNumber": func(n interface{}) string {
			switch v := n.(type) {
			case int:
				return printer.Sprintf("%d", v)
			case float64:
				return printer.Sprintf("%.0f", v)
			default:
				return printer.Sprintf("%v", v)
			}
		},
		"neighborList": func(neighbors []Neighbor) string {
			sorted := make([]Neighbor, len(neighbors))
			copy(sorted, neighbors)
			sort.Slice(sorted, func(i, j int) bool {
				return strings.ToLower(sorted[i].Hostname) < strings.ToLower(sorted[j].Hostname)
			})
			parts := make([]string, len(sorted))
			for i, n := range sorted {
				parts[i] = printer.Sprintf("%s (%d)", n.Hostname, n.Metric)
			}
			return strings.Join(parts, ", ")
		},
		"sortNeighbors": func(neighbors []Neighbor) []Neighbor {
			sorted := make([]Neighbor, len(neighbors))
			copy(sorted, neighbors)
			sort.Slice(sorted, func(i, j int) bool {
				return strings.ToLower(sorted[i].Hostname) < strings.ToLower(sorted[j].Hostname)
			})
			return sorted
		},
		"sortReachabilities": func(reachabilities []Reachability) []Reachability {
			sorted := make([]Reachability, len(reachabilities))
			copy(sorted, reachabilities)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Prefix < sorted[j].Prefix
			})
			return sorted
		},
		"routerTypeDesc": func(t string) string {
			switch t {
			case "L1":
				return "Level 1 (intra-area only)"
			case "L2":
				return "Level 2 (inter-area backbone)"
			case "L1L2":
				return "Level 1/2 (both intra and inter-area)"
			default:
				return t
			}
		},
		"srInfo": func(r Reachability) string {
			if r.SR == nil {
				return ""
			}
			if r.SR.IsNodeSID {
				return printer.Sprintf(", Node SID=%d", r.SR.SID)
			}
			return printer.Sprintf(", SID=%d", r.SR.SID)
		},
	}

	return template.New("report").Funcs(funcMap).Parse(reportTemplate)
}

// generateMarkdown renders the report template with the given data.
func generateMarkdown(routers map[string]Router, stats NetworkStats, timestamp string, locator *Locator) (string, error) {
	tmpl, err := newReportTemplate()
	if err != nil {
		return "", err
	}

	// Sort hostnames alphabetically (case-insensitive)
	sortedHostnames := make([]string, 0, len(routers))
	for hostname := range routers {
		sortedHostnames = append(sortedHostnames, hostname)
	}
	sort.Slice(sortedHostnames, func(i, j int) bool {
		return strings.ToLower(sortedHostnames[i]) < strings.ToLower(sortedHostnames[j])
	})

	// Build neighbor buckets
	neighborBuckets := buildNeighborBuckets(routers, sortedHostnames)

	// Build location buckets
	locationBuckets := buildLocationBuckets(routers, locator)

	data := templateData{
		Timestamp:       timestamp,
		Stats:           stats,
		Routers:         routers,
		SortedHostnames: sortedHostnames,
		NeighborBuckets: neighborBuckets,
		LocationBuckets: locationBuckets,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}

	return buf.String(), nil
}

func buildNeighborBuckets(routers map[string]Router, sortedHostnames []string) []NeighborBucket {
	// Group by neighbor count
	buckets := make(map[int][]string)
	for _, hostname := range sortedHostnames {
		count := len(routers[hostname].Neighbors)
		buckets[count] = append(buckets[count], hostname)
	}

	// Sort bucket counts descending
	counts := make([]int, 0, len(buckets))
	for count := range buckets {
		counts = append(counts, count)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(counts)))

	printer := message.NewPrinter(language.English)
	result := make([]NeighborBucket, 0, len(counts))
	for _, count := range counts {
		hostnames := buckets[count]
		sort.Slice(hostnames, func(i, j int) bool {
			return strings.ToLower(hostnames[i]) < strings.ToLower(hostnames[j])
		})

		var label string
		if count >= 5 {
			label = "5+ neighbors"
		} else if count == 1 {
			label = "1 neighbor"
		} else {
			label = printer.Sprintf("%d neighbors", count)
		}

		// Format: hostname (count)
		parts := make([]string, len(hostnames))
		for i, h := range hostnames {
			parts[i] = printer.Sprintf("%s (%d)", h, len(routers[h].Neighbors))
		}

		result = append(result, NeighborBucket{
			Label:   label,
			Routers: strings.Join(parts, ", "),
		})
	}

	return result
}

func buildLocationBuckets(routers map[string]Router, locator *Locator) []LocationBucket {
	// Group by location
	locationRouters := make(map[string][]string)
	for hostname, router := range routers {
		locationRouters[router.Location] = append(locationRouters[router.Location], hostname)
	}

	// Sort locations, put "Other" last
	locations := make([]string, 0, len(locationRouters))
	for loc := range locationRouters {
		locations = append(locations, loc)
	}
	sort.Slice(locations, func(i, j int) bool {
		if locations[i] == "Other" {
			return false
		}
		if locations[j] == "Other" {
			return true
		}
		return strings.ToLower(locations[i]) < strings.ToLower(locations[j])
	})

	result := make([]LocationBucket, 0, len(locations))
	for _, loc := range locations {
		hostnames := locationRouters[loc]
		sort.Slice(hostnames, func(i, j int) bool {
			return strings.ToLower(hostnames[i]) < strings.ToLower(hostnames[j])
		})

		result = append(result, LocationBucket{
			Location:    loc,
			Description: locator.Description(loc),
			Routers:     strings.Join(hostnames, ", "),
		})
	}

	return result
}
