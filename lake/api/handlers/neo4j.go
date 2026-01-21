package handlers

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/malbeclabs/doublezero/lake/indexer/pkg/neo4j"
	neo4jdriver "github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// Neo4jQuerier implements workflow.Querier for Neo4j graph queries.
type Neo4jQuerier struct{}

// NewNeo4jQuerier creates a new Neo4jQuerier.
func NewNeo4jQuerier() *Neo4jQuerier {
	return &Neo4jQuerier{}
}

// Query executes a Cypher query and returns formatted results.
func (q *Neo4jQuerier) Query(ctx context.Context, cypher string) (workflow.QueryResult, error) {
	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	result, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, cypher, nil)
		if err != nil {
			return nil, err
		}

		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}

		// Get column names from keys
		var columns []string
		if len(records) > 0 {
			columns = records[0].Keys
		}

		// Convert records to row maps
		rows := make([]map[string]any, 0, len(records))
		for _, record := range records {
			row := make(map[string]any)
			for _, key := range record.Keys {
				val, _ := record.Get(key)
				row[key] = convertNeo4jValue(val)
			}
			rows = append(rows, row)
		}

		return workflow.QueryResult{
			SQL:       cypher,
			Columns:   columns,
			Rows:      rows,
			Count:     len(rows),
			Formatted: formatCypherResult(columns, rows),
		}, nil
	})

	if err != nil {
		return workflow.QueryResult{
			SQL:   cypher,
			Error: err.Error(),
		}, nil
	}

	return result.(workflow.QueryResult), nil
}

// convertNeo4jValue converts Neo4j types to standard Go types.
func convertNeo4jValue(val any) any {
	if val == nil {
		return nil
	}

	switch v := val.(type) {
	case neo4jdriver.Node:
		// Convert Node to a map with labels and properties
		props := make(map[string]any)
		for k, pv := range v.Props {
			props[k] = convertNeo4jValue(pv)
		}
		return map[string]any{
			"_labels":     v.Labels,
			"_properties": props,
		}
	case neo4jdriver.Relationship:
		// Convert Relationship to a map
		props := make(map[string]any)
		for k, pv := range v.Props {
			props[k] = convertNeo4jValue(pv)
		}
		return map[string]any{
			"_type":       v.Type,
			"_properties": props,
		}
	case neo4jdriver.Path:
		// Convert Path to nodes and relationships
		nodes := make([]any, len(v.Nodes))
		for i, n := range v.Nodes {
			nodes[i] = convertNeo4jValue(n)
		}
		rels := make([]any, len(v.Relationships))
		for i, r := range v.Relationships {
			rels[i] = convertNeo4jValue(r)
		}
		return map[string]any{
			"_nodes":         nodes,
			"_relationships": rels,
		}
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = convertNeo4jValue(item)
		}
		return result
	case map[string]any:
		result := make(map[string]any)
		for k, mv := range v {
			result[k] = convertNeo4jValue(mv)
		}
		return result
	case float64:
		// Sanitize NaN/Inf which aren't valid JSON
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return v
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil
		}
		return v
	default:
		return v
	}
}

// formatCypherResult formats query results for display.
func formatCypherResult(columns []string, rows []map[string]any) string {
	if len(rows) == 0 {
		return "(no results)"
	}

	var sb strings.Builder

	// Header
	sb.WriteString("| ")
	for i, col := range columns {
		if i > 0 {
			sb.WriteString(" | ")
		}
		sb.WriteString(col)
	}
	sb.WriteString(" |\n")

	// Separator
	sb.WriteString("|")
	for range columns {
		sb.WriteString("---|")
	}
	sb.WriteString("\n")

	// Rows (limit to 50 for readability)
	maxRows := 50
	for i, row := range rows {
		if i >= maxRows {
			sb.WriteString(fmt.Sprintf("\n... and %d more rows", len(rows)-maxRows))
			break
		}
		sb.WriteString("| ")
		for j, col := range columns {
			if j > 0 {
				sb.WriteString(" | ")
			}
			sb.WriteString(formatNeo4jValue(row[col]))
		}
		sb.WriteString(" |\n")
	}

	return sb.String()
}

// formatNeo4jValue formats a single value for display, optimized for agent understanding.
func formatNeo4jValue(v any) string {
	if v == nil {
		return ""
	}
	switch val := v.(type) {
	case string:
		if len(val) > 50 {
			return val[:47] + "..."
		}
		return val
	case []any:
		if len(val) == 0 {
			return "[]"
		}
		// Check if this looks like a path (array of objects with type: device/link)
		if isPathArray(val) {
			return formatPathArray(val)
		}
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, formatNeo4jValue(item))
		}
		result := "[" + strings.Join(parts, ", ") + "]"
		if len(result) > 80 {
			return result[:77] + "..."
		}
		return result
	case map[string]any:
		// Check for Neo4j node representation
		if labels, ok := val["_labels"]; ok {
			return formatNeo4jNode(val, labels)
		}
		// Check for Neo4j relationship representation
		if relType, ok := val["_type"]; ok {
			return formatNeo4jRelationship(val, relType)
		}
		// Check for Neo4j path representation
		if nodes, ok := val["_nodes"]; ok {
			if rels, ok := val["_relationships"]; ok {
				return formatNeo4jPath(nodes, rels)
			}
		}
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%v", val)
	}
}

// formatNeo4jNode formats a Neo4j node for agent-friendly display.
// Output: "Device(NYC-CORE-01)" or "Metro(NYC)"
func formatNeo4jNode(val map[string]any, labels any) string {
	// Get primary label
	label := "Node"
	if labelsArr, ok := labels.([]any); ok && len(labelsArr) > 0 {
		label = fmt.Sprintf("%v", labelsArr[0])
	}

	// Get identifying property
	identifier := getNodeIdentifier(val)
	if identifier != "" {
		return fmt.Sprintf("%s(%s)", label, identifier)
	}
	return fmt.Sprintf("%s(?)", label)
}

// formatNeo4jRelationship formats a Neo4j relationship for agent-friendly display.
// Output: "[:ISIS_ADJACENT {metric: 100}]"
func formatNeo4jRelationship(val map[string]any, relType any) string {
	typeStr := fmt.Sprintf("%v", relType)

	// Get key properties to show
	props, _ := val["_properties"].(map[string]any)
	if len(props) == 0 {
		return fmt.Sprintf("[:%s]", typeStr)
	}

	// Show important properties (metric, weight, cost, name)
	propParts := make([]string, 0, 2)
	priorityKeys := []string{"metric", "weight", "cost", "name", "type"}
	for _, key := range priorityKeys {
		if v, ok := props[key]; ok && len(propParts) < 2 {
			propParts = append(propParts, fmt.Sprintf("%s: %v", key, v))
		}
	}

	if len(propParts) > 0 {
		return fmt.Sprintf("[:%s {%s}]", typeStr, strings.Join(propParts, ", "))
	}
	return fmt.Sprintf("[:%s]", typeStr)
}

// isPathArray checks if an array looks like a path (alternating device/link objects).
func isPathArray(arr []any) bool {
	if len(arr) < 2 {
		return false
	}
	// Check first few elements for device/link pattern
	for i := 0; i < len(arr) && i < 4; i++ {
		obj, ok := arr[i].(map[string]any)
		if !ok {
			return false
		}
		typeVal, hasType := obj["type"]
		_, hasCode := obj["code"]
		if !hasType || !hasCode {
			return false
		}
		typeStr, ok := typeVal.(string)
		if !ok {
			return false
		}
		if typeStr != "device" && typeStr != "link" {
			return false
		}
	}
	return true
}

// formatPathArray formats an array of path elements (device/link objects) as a visual path.
// Output: "device1 → link1 → device2" or with details
func formatPathArray(arr []any) string {
	parts := make([]string, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		code, _ := obj["code"].(string)
		itemType, _ := obj["type"].(string)

		if itemType == "device" {
			parts = append(parts, code)
		} else if itemType == "link" {
			// For links, show a simpler arrow representation
			parts = append(parts, "→")
		}
	}
	return strings.Join(parts, " ")
}

// formatNeo4jPath formats a Neo4j path for agent-friendly display.
// Output: "[Device(A) -[:HAS_LINK]-> Link(L1) -[:HAS_LINK]-> Device(B)]"
func formatNeo4jPath(nodes any, rels any) string {
	nodesArr, ok1 := nodes.([]any)
	relsArr, ok2 := rels.([]any)
	if !ok1 || !ok2 || len(nodesArr) == 0 {
		return "[empty path]"
	}

	parts := make([]string, 0, len(nodesArr)*2)
	for i, node := range nodesArr {
		nodeMap, ok := node.(map[string]any)
		if ok {
			labels := nodeMap["_labels"]
			parts = append(parts, formatNeo4jNode(nodeMap, labels))
		} else {
			parts = append(parts, formatNeo4jValue(node))
		}

		if i < len(relsArr) {
			relMap, ok := relsArr[i].(map[string]any)
			if ok {
				relType := relMap["_type"]
				parts = append(parts, fmt.Sprintf("-[:%v]->", relType))
			} else {
				parts = append(parts, "->")
			}
		}
	}

	return "[" + strings.Join(parts, " ") + "]"
}

// getNodeIdentifier extracts the identifying property from a node.
// Checks: code, name, pk, id in priority order.
func getNodeIdentifier(val map[string]any) string {
	props, _ := val["_properties"].(map[string]any)
	if props == nil {
		return ""
	}

	// Priority order for identifying properties
	candidates := []string{"code", "name", "pk", "id"}
	for _, key := range candidates {
		if v, ok := props[key]; ok && v != nil {
			return fmt.Sprintf("%v", v)
		}
	}

	// Fallback to first property
	for _, v := range props {
		if v != nil {
			s := fmt.Sprintf("%v", v)
			if len(s) > 30 {
				return s[:27] + "..."
			}
			return s
		}
	}
	return ""
}

// Neo4jSchemaFetcher implements workflow.SchemaFetcher for Neo4j.
type Neo4jSchemaFetcher struct{}

// NewNeo4jSchemaFetcher creates a new Neo4jSchemaFetcher.
func NewNeo4jSchemaFetcher() *Neo4jSchemaFetcher {
	return &Neo4jSchemaFetcher{}
}

// FetchSchema returns a formatted string describing the Neo4j graph schema.
func (f *Neo4jSchemaFetcher) FetchSchema(ctx context.Context) (string, error) {
	session := config.Neo4jSession(ctx)
	defer session.Close(ctx)

	var sb strings.Builder
	sb.WriteString("## Graph Database Schema (Neo4j)\n\n")

	// Get node labels and their properties
	labels, err := f.getNodeLabels(ctx, session)
	if err != nil {
		return "", fmt.Errorf("failed to get node labels: %w", err)
	}

	if len(labels) > 0 {
		sb.WriteString("### Node Labels\n\n")
		for _, label := range labels {
			sb.WriteString(fmt.Sprintf("**%s**\n", label.Name))
			if len(label.Properties) > 0 {
				sb.WriteString("Properties:\n")
				for _, prop := range label.Properties {
					sb.WriteString(fmt.Sprintf("- `%s` (%s)\n", prop.Name, prop.Type))
				}
			}
			sb.WriteString("\n")
		}
	}

	// Get relationship types
	relTypes, err := f.getRelationshipTypes(ctx, session)
	if err != nil {
		return "", fmt.Errorf("failed to get relationship types: %w", err)
	}

	if len(relTypes) > 0 {
		sb.WriteString("### Relationship Types\n\n")
		for _, rel := range relTypes {
			sb.WriteString(fmt.Sprintf("- `%s`", rel.Name))
			if len(rel.Properties) > 0 {
				propNames := make([]string, len(rel.Properties))
				for i, p := range rel.Properties {
					propNames[i] = p.Name
				}
				sb.WriteString(fmt.Sprintf(" (properties: %s)", strings.Join(propNames, ", ")))
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

type labelInfo struct {
	Name       string
	Properties []propertyInfo
}

type propertyInfo struct {
	Name string
	Type string
}

type relTypeInfo struct {
	Name       string
	Properties []propertyInfo
}

func (f *Neo4jSchemaFetcher) getNodeLabels(ctx context.Context, session neo4j.Session) ([]labelInfo, error) {
	// Get labels
	labelsResult, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, "CALL db.labels()", nil)
		if err != nil {
			return nil, err
		}
		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}
		labels := make([]string, 0, len(records))
		for _, record := range records {
			if label, ok := record.Values[0].(string); ok {
				labels = append(labels, label)
			}
		}
		return labels, nil
	})
	if err != nil {
		return nil, err
	}

	labels := labelsResult.([]string)

	// Get properties for each label using schema.nodeTypeProperties if available
	propsResult, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, "CALL db.schema.nodeTypeProperties()", nil)
		if err != nil {
			// Fall back if procedure doesn't exist
			return nil, nil
		}
		records, err := res.Collect(ctx)
		if err != nil {
			return nil, nil
		}

		// Build label -> properties map
		propMap := make(map[string][]propertyInfo)
		for _, record := range records {
			nodeLabels, _ := record.Get("nodeLabels")
			propName, _ := record.Get("propertyName")
			propTypes, _ := record.Get("propertyTypes")

			if labelsArr, ok := nodeLabels.([]any); ok && len(labelsArr) > 0 {
				labelName := fmt.Sprintf("%v", labelsArr[0])
				propNameStr := fmt.Sprintf("%v", propName)
				propTypeStr := "any"
				if typesArr, ok := propTypes.([]any); ok && len(typesArr) > 0 {
					propTypeStr = fmt.Sprintf("%v", typesArr[0])
				}
				propMap[labelName] = append(propMap[labelName], propertyInfo{
					Name: propNameStr,
					Type: propTypeStr,
				})
			}
		}
		return propMap, nil
	})

	propMap := make(map[string][]propertyInfo)
	if propsResult != nil {
		propMap = propsResult.(map[string][]propertyInfo)
	}

	result := make([]labelInfo, 0, len(labels))
	for _, label := range labels {
		result = append(result, labelInfo{
			Name:       label,
			Properties: propMap[label],
		})
	}

	return result, nil
}

func (f *Neo4jSchemaFetcher) getRelationshipTypes(ctx context.Context, session neo4j.Session) ([]relTypeInfo, error) {
	// Get relationship types
	typesResult, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, "CALL db.relationshipTypes()", nil)
		if err != nil {
			return nil, err
		}
		records, err := res.Collect(ctx)
		if err != nil {
			return nil, err
		}
		types := make([]string, 0, len(records))
		for _, record := range records {
			if relType, ok := record.Values[0].(string); ok {
				types = append(types, relType)
			}
		}
		return types, nil
	})
	if err != nil {
		return nil, err
	}

	relTypes := typesResult.([]string)

	// Get properties for each relationship type
	propsResult, err := session.ExecuteRead(ctx, func(tx neo4j.Transaction) (any, error) {
		res, err := tx.Run(ctx, "CALL db.schema.relTypeProperties()", nil)
		if err != nil {
			return nil, nil
		}
		records, err := res.Collect(ctx)
		if err != nil {
			return nil, nil
		}

		propMap := make(map[string][]propertyInfo)
		for _, record := range records {
			relType, _ := record.Get("relType")
			propName, _ := record.Get("propertyName")
			propTypes, _ := record.Get("propertyTypes")

			relTypeStr := strings.TrimPrefix(fmt.Sprintf("%v", relType), ":`")
			relTypeStr = strings.TrimSuffix(relTypeStr, "`")
			propNameStr := fmt.Sprintf("%v", propName)
			propTypeStr := "any"
			if typesArr, ok := propTypes.([]any); ok && len(typesArr) > 0 {
				propTypeStr = fmt.Sprintf("%v", typesArr[0])
			}
			propMap[relTypeStr] = append(propMap[relTypeStr], propertyInfo{
				Name: propNameStr,
				Type: propTypeStr,
			})
		}
		return propMap, nil
	})

	propMap := make(map[string][]propertyInfo)
	if propsResult != nil {
		propMap = propsResult.(map[string][]propertyInfo)
	}

	result := make([]relTypeInfo, 0, len(relTypes))
	for _, relType := range relTypes {
		result = append(result, relTypeInfo{
			Name:       relType,
			Properties: propMap[relType],
		})
	}

	return result, nil
}
