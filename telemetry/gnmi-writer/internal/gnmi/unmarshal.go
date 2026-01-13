package gnmi

import (
	"encoding/json"
	"fmt"
	"strings"

	gpb "github.com/openconfig/gnmi/proto/gnmi"
	"github.com/openconfig/ygot/ytypes"

	"github.com/malbeclabs/doublezero/telemetry/gnmi-writer/internal/gnmi/oc"
)

// listSchemaCache maps "containerName/listName" to schema entry name.
// Built once at initialization to avoid O(n) schema lookups on each notification.
type listSchemaCache map[string]string

// buildListSchemaCache builds a cache mapping container/list paths to schema names.
// This is called once at startup and enables O(1) lookups during notification processing.
func buildListSchemaCache(schema *ytypes.Schema) listSchemaCache {
	cache := make(listSchemaCache)

	for name, entry := range schema.SchemaTree {
		if entry == nil || entry.Key == "" {
			continue // Skip non-list entries
		}

		pathStr := entry.Path()
		if pathStr == "" {
			continue
		}

		// Extract the last two path elements (container/list)
		// e.g., "/network-instance/.../adjacencies/adjacency" -> "adjacencies/adjacency"
		parts := strings.Split(strings.Trim(pathStr, "/"), "/")
		if len(parts) >= 2 {
			containerName := parts[len(parts)-2]
			listName := parts[len(parts)-1]
			key := containerName + "/" + listName
			cache[key] = name
		}
	}

	return cache
}

// unmarshalNotification unmarshals a gNMI notification update into an oc.Device.
// It handles the case where the path ends at a container holding a list (e.g., "adjacencies")
// by detecting the list in the JSON and unmarshalling each element individually.
//
// This is necessary because ygot's compressed schema (used for performance) combines
// container/list paths (e.g., "adjacencies/adjacency" becomes a single path tag).
// When gNMI returns a path ending at the container (e.g., "adjacencies") with JSON
// containing the list elements, ygot's SetNode may silently ignore the list data
// even when using IgnoreExtraFields.
func (p *Processor) unmarshalNotification(notification *gpb.Notification, update *gpb.Update) (*oc.Device, error) {
	device := &oc.Device{}
	fullPath := mergePaths(notification.GetPrefix(), update.GetPath())
	val := update.GetVal()

	// Check if we have JSON with a list - if so, we need special handling
	jsonVal := val.GetJsonIetfVal()
	if jsonVal != nil {
		var jsonData map[string]any
		if err := json.Unmarshal(jsonVal, &jsonData); err != nil {
			return nil, fmt.Errorf("error parsing JSON: %w", err)
		}

		// If the JSON contains a list (array), use the list unmarshalling approach
		// This is required because SetNode may silently skip list data in compressed schemas
		listKey, listData, found := findListInJSON(jsonData)
		if found && len(listData) > 0 {
			return p.unmarshalListNotification(device, fullPath, listKey, listData)
		}
	}

	// For non-list JSON or scalar values, use direct SetNode
	err := ytypes.SetNode(
		p.schema.SchemaTree["Device"],
		device,
		fullPath,
		val,
		&ytypes.InitMissingElements{},
		&ytypes.IgnoreExtraFields{},
	)
	if err != nil {
		return nil, fmt.Errorf("SetNode failed: %w", err)
	}

	return device, nil
}

// unmarshalListNotification handles JSON containing a list by unmarshalling each element individually.
func (p *Processor) unmarshalListNotification(device *oc.Device, fullPath *gpb.Path, listKey string, listData []any) (*oc.Device, error) {
	if len(fullPath.GetElem()) == 0 {
		return nil, fmt.Errorf("path is empty, cannot determine parent")
	}

	// Get the parent path (container's parent) and the container name
	parentPath := &gpb.Path{
		Origin: fullPath.GetOrigin(),
		Target: fullPath.GetTarget(),
		Elem:   fullPath.GetElem()[:len(fullPath.GetElem())-1],
	}
	containerName := fullPath.GetElem()[len(fullPath.GetElem())-1].GetName()

	// Initialize the parent path structure
	err := ytypes.SetNode(
		p.schema.SchemaTree["Device"],
		device,
		parentPath,
		nil,
		&ytypes.InitMissingElements{},
	)
	if err != nil {
		return nil, fmt.Errorf("error initializing parent path: %w", err)
	}

	// Find the schema for the list element using the cache (O(1) lookup)
	cacheKey := containerName + "/" + listKey
	listSchemaName := p.listCache[cacheKey]
	if listSchemaName == "" {
		return nil, fmt.Errorf("could not find schema for list %q in container %q", listKey, containerName)
	}

	listSchema := p.schema.SchemaTree[listSchemaName]
	if listSchema == nil {
		return nil, fmt.Errorf("schema %q not found in SchemaTree", listSchemaName)
	}

	// Unmarshal each list element and add to the device using SetNode
	for i, elem := range listData {
		elemMap, ok := elem.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("list element %d is not an object", i)
		}

		// Wrap the element in the expected JSON structure for SetNode
		wrappedJSON, err := wrapJSONForSetNode(containerName, listKey, elemMap)
		if err != nil {
			return nil, fmt.Errorf("error wrapping list element %d: %w", i, err)
		}
		wrappedVal := &gpb.TypedValue{
			Value: &gpb.TypedValue_JsonIetfVal{
				JsonIetfVal: wrappedJSON,
			},
		}

		err = ytypes.SetNode(
			p.schema.SchemaTree["Device"],
			device,
			parentPath,
			wrappedVal,
			&ytypes.InitMissingElements{},
			&ytypes.IgnoreExtraFields{},
		)
		if err != nil {
			return nil, fmt.Errorf("error unmarshalling list element %d: %w", i, err)
		}
	}

	return device, nil
}

// findListInJSON looks for a key in the JSON that maps to an array.
// Returns the key name (without module prefix), the array data, and whether it was found.
func findListInJSON(data map[string]any) (string, []any, bool) {
	for key, val := range data {
		if arr, ok := val.([]any); ok {
			// Strip module prefix (e.g., "openconfig-network-instance:adjacency" -> "adjacency")
			cleanKey := key
			if idx := strings.LastIndex(key, ":"); idx != -1 {
				cleanKey = key[idx+1:]
			}
			return cleanKey, arr, true
		}
	}
	return "", nil, false
}

// wrapJSONForSetNode wraps a list element in the container structure expected by SetNode.
func wrapJSONForSetNode(containerName, listName string, elemData map[string]any) ([]byte, error) {
	// Wrap as: {"container": {"list": [element]}}
	wrapped := map[string]any{
		containerName: map[string]any{
			listName: []any{elemData},
		},
	}
	data, err := json.Marshal(wrapped)
	if err != nil {
		return nil, fmt.Errorf("error marshalling wrapped JSON: %w", err)
	}
	return data, nil
}

// mergePaths combines a prefix path and an update path into a single path.
func mergePaths(prefix, path *gpb.Path) *gpb.Path {
	if prefix == nil {
		return path
	}
	if path == nil {
		return prefix
	}

	result := &gpb.Path{
		Origin: prefix.GetOrigin(),
		Target: prefix.GetTarget(),
		Elem:   make([]*gpb.PathElem, 0, len(prefix.GetElem())+len(path.GetElem())),
	}
	result.Elem = append(result.Elem, prefix.GetElem()...)
	result.Elem = append(result.Elem, path.GetElem()...)
	return result
}
