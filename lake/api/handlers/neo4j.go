package handlers

import (
	"context"
	"fmt"
	"strings"

	"github.com/malbeclabs/doublezero/lake/agent/pkg/workflow"
	"github.com/malbeclabs/doublezero/lake/api/config"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
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

	result, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
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
	case neo4j.Node:
		// Convert Node to a map with labels and properties
		props := make(map[string]any)
		for k, pv := range v.Props {
			props[k] = convertNeo4jValue(pv)
		}
		return map[string]any{
			"_labels":     v.Labels,
			"_properties": props,
		}
	case neo4j.Relationship:
		// Convert Relationship to a map
		props := make(map[string]any)
		for k, pv := range v.Props {
			props[k] = convertNeo4jValue(pv)
		}
		return map[string]any{
			"_type":       v.Type,
			"_properties": props,
		}
	case neo4j.Path:
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

// formatNeo4jValue formats a single value for display.
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
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, formatNeo4jValue(item))
		}
		result := "[" + strings.Join(parts, ", ") + "]"
		if len(result) > 50 {
			return result[:47] + "..."
		}
		return result
	case map[string]any:
		// Check for Neo4j node/relationship representation
		if labels, ok := val["_labels"]; ok {
			return fmt.Sprintf("Node%v", labels)
		}
		if relType, ok := val["_type"]; ok {
			return fmt.Sprintf("[:%s]", relType)
		}
		return fmt.Sprintf("%v", val)
	default:
		return fmt.Sprintf("%v", val)
	}
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

func (f *Neo4jSchemaFetcher) getNodeLabels(ctx context.Context, session neo4j.SessionWithContext) ([]labelInfo, error) {
	// Get labels
	labelsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
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
	propsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
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

func (f *Neo4jSchemaFetcher) getRelationshipTypes(ctx context.Context, session neo4j.SessionWithContext) ([]relTypeInfo, error) {
	// Get relationship types
	typesResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
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
	propsResult, err := session.ExecuteRead(ctx, func(tx neo4j.ManagedTransaction) (any, error) {
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
