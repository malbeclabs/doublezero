/**
 * Neo4j utility functions for type detection, formatting, and graph extraction.
 * Used to display Neo4j/Cypher query results in a human-readable format.
 */

// ============================================================================
// Type Definitions
// ============================================================================

/** Represents a Neo4j Node as returned by the backend */
export interface Neo4jNode {
  _labels: string[]
  _properties: Record<string, unknown>
}

/** Represents a Neo4j Relationship as returned by the backend */
export interface Neo4jRelationship {
  _type: string
  _properties: Record<string, unknown>
}

/** Represents a Neo4j Path as returned by the backend */
export interface Neo4jPath {
  _nodes: Neo4jNode[]
  _relationships: Neo4jRelationship[]
}

/** Graph data structure for visualization */
export interface GraphData {
  nodes: GraphNode[]
  edges: GraphEdge[]
}

export interface GraphNode {
  id: string
  labels: string[]
  properties: Record<string, unknown>
  displayName: string
}

export interface GraphEdge {
  id: string
  source: string
  target: string
  type: string
  properties: Record<string, unknown>
}

// ============================================================================
// Type Guards
// ============================================================================

/** Check if a value is a Neo4j Node */
export function isNeo4jNode(value: unknown): value is Neo4jNode {
  if (!value || typeof value !== 'object') return false
  const obj = value as Record<string, unknown>
  return (
    Array.isArray(obj._labels) &&
    obj._properties !== undefined &&
    typeof obj._properties === 'object'
  )
}

/** Check if a value is a Neo4j Relationship */
export function isNeo4jRelationship(value: unknown): value is Neo4jRelationship {
  if (!value || typeof value !== 'object') return false
  const obj = value as Record<string, unknown>
  return (
    typeof obj._type === 'string' &&
    obj._properties !== undefined &&
    typeof obj._properties === 'object'
  )
}

/** Check if a value is a Neo4j Path */
export function isNeo4jPath(value: unknown): value is Neo4jPath {
  if (!value || typeof value !== 'object') return false
  const obj = value as Record<string, unknown>
  return (
    Array.isArray(obj._nodes) &&
    Array.isArray(obj._relationships) &&
    obj._nodes.every(isNeo4jNode)
  )
}

/** Path element from shortestPath projection (device or link with code) */
interface PathElement {
  type: 'device' | 'link'
  code: string
  [key: string]: unknown
}

/** Check if an array is a path projection (array of device/link objects) */
export function isPathArray(value: unknown): value is PathElement[] {
  if (!Array.isArray(value) || value.length < 2) return false
  // Check first few elements to see if they match the pattern
  for (let i = 0; i < Math.min(value.length, 4); i++) {
    const item = value[i]
    if (!item || typeof item !== 'object') return false
    const obj = item as Record<string, unknown>
    if (typeof obj.type !== 'string' || typeof obj.code !== 'string') return false
    if (obj.type !== 'device' && obj.type !== 'link') return false
  }
  return true
}

/** Format a path array as "device1 → device2 → device3" */
export function formatPathArray(arr: PathElement[]): string {
  const parts: string[] = []
  for (const item of arr) {
    if (item.type === 'device') {
      parts.push(item.code)
    } else if (item.type === 'link') {
      parts.push('→')
    }
  }
  return parts.join(' ')
}

/** Check if a value contains any Neo4j graph objects */
export function isNeo4jValue(value: unknown): boolean {
  return isNeo4jNode(value) || isNeo4jRelationship(value) || isNeo4jPath(value) || isPathArray(value)
}

// ============================================================================
// Display Formatters
// ============================================================================

/** Get the identifying property of a node (code, name, pk, or first property) */
function getNodeIdentifier(node: Neo4jNode): string {
  const props = node._properties
  // Priority: code > name > pk > id > first property
  const candidates = ['code', 'name', 'pk', 'id']
  for (const key of candidates) {
    if (props[key] !== undefined && props[key] !== null) {
      return String(props[key])
    }
  }
  // Fall back to first property
  const keys = Object.keys(props)
  if (keys.length > 0) {
    return String(props[keys[0]])
  }
  return '?'
}

/** Get the primary label for a node */
function getPrimaryLabel(node: Neo4jNode): string {
  if (node._labels.length === 0) return 'Node'
  // Return the first non-internal label
  return node._labels.find(l => !l.startsWith('_')) || node._labels[0]
}

/** Format a Neo4j Node for display: "Device: NYC-CORE-01" */
export function formatNeo4jNode(node: Neo4jNode): string {
  const label = getPrimaryLabel(node)
  const id = getNodeIdentifier(node)
  return `${label}: ${id}`
}

/** Format a Neo4j Relationship for display: "[:ISIS_ADJACENT {metric: 100}]" */
export function formatNeo4jRelationship(rel: Neo4jRelationship): string {
  const props = rel._properties
  const propKeys = Object.keys(props)

  if (propKeys.length === 0) {
    return `[:${rel._type}]`
  }

  // Show up to 2 key properties
  const propParts: string[] = []
  const priorityKeys = ['metric', 'weight', 'cost', 'name', 'type']
  const keysToShow = priorityKeys.filter(k => props[k] !== undefined).slice(0, 2)

  if (keysToShow.length === 0) {
    keysToShow.push(...propKeys.slice(0, 2))
  }

  for (const key of keysToShow) {
    if (props[key] !== undefined) {
      propParts.push(`${key}: ${props[key]}`)
    }
  }

  if (propParts.length > 0) {
    return `[:${rel._type} {${propParts.join(', ')}}]`
  }
  return `[:${rel._type}]`
}

/** Format a Neo4j Path for display: "Device:A -> [:CONNECTS] -> Device:B" */
export function formatNeo4jPath(path: Neo4jPath): string {
  const parts: string[] = []

  for (let i = 0; i < path._nodes.length; i++) {
    parts.push(formatNeo4jNode(path._nodes[i]))

    if (i < path._relationships.length) {
      parts.push(`-[:${path._relationships[i]._type}]->`)
    }
  }

  return parts.join(' ')
}

/** Format any Neo4j value for human-readable display */
export function formatNeo4jValue(value: unknown): string {
  if (value === null || value === undefined) {
    return ''
  }

  if (isNeo4jNode(value)) {
    return formatNeo4jNode(value)
  }

  if (isNeo4jRelationship(value)) {
    return formatNeo4jRelationship(value)
  }

  if (isNeo4jPath(value)) {
    return formatNeo4jPath(value)
  }

  // Check for path arrays before generic array handling
  if (isPathArray(value)) {
    return formatPathArray(value)
  }

  if (Array.isArray(value)) {
    // Handle arrays of Neo4j values
    const formatted = value.map(v => formatNeo4jValue(v))
    return `[${formatted.join(', ')}]`
  }

  if (typeof value === 'object') {
    // Regular object - stringify but keep it short
    const str = JSON.stringify(value)
    if (str.length > 50) {
      return str.slice(0, 47) + '...'
    }
    return str
  }

  return String(value)
}

// ============================================================================
// Graph Data Extraction
// ============================================================================

/** Check if query results contain any graph data (nodes, relationships, or paths) */
export function containsGraphData(rows: unknown[][]): boolean {
  for (const row of rows) {
    for (const cell of row) {
      if (isNeo4jNode(cell) || isNeo4jRelationship(cell) || isNeo4jPath(cell)) {
        return true
      }
      // Check for path arrays
      if (isPathArray(cell)) {
        return true
      }
      // Check arrays
      if (Array.isArray(cell)) {
        for (const item of cell) {
          if (isNeo4jNode(item) || isNeo4jRelationship(item) || isNeo4jPath(item)) {
            return true
          }
        }
      }
    }
  }
  return false
}

/** Generate a unique ID for a node based on its properties */
function generateNodeId(node: Neo4jNode): string {
  const props = node._properties
  // Use identifying properties or create a hash from all properties
  const id = props.pk || props.id || props.code || props.name
  if (id !== undefined) {
    return `${getPrimaryLabel(node)}-${id}`
  }
  // Fallback: create ID from stringified properties
  return `${getPrimaryLabel(node)}-${JSON.stringify(props)}`
}

/** Extract graph data from query results for visualization */
export function extractGraphData(rows: unknown[][]): GraphData {
  const nodesMap = new Map<string, GraphNode>()
  const edgesMap = new Map<string, GraphEdge>()
  let edgeIdCounter = 0

  function addNode(node: Neo4jNode): string {
    const id = generateNodeId(node)
    if (!nodesMap.has(id)) {
      nodesMap.set(id, {
        id,
        labels: node._labels,
        properties: node._properties as Record<string, unknown>,
        displayName: getNodeIdentifier(node),
      })
    }
    return id
  }

  function addEdge(sourceId: string, targetId: string, rel: Neo4jRelationship): void {
    // Create a deterministic edge ID
    const edgeKey = `${sourceId}-${rel._type}-${targetId}`
    if (!edgesMap.has(edgeKey)) {
      edgesMap.set(edgeKey, {
        id: `edge-${edgeIdCounter++}`,
        source: sourceId,
        target: targetId,
        type: rel._type,
        properties: rel._properties as Record<string, unknown>,
      })
    }
  }

  function processValue(value: unknown): void {
    if (isNeo4jNode(value)) {
      addNode(value)
    } else if (isNeo4jPath(value)) {
      // Process path: add nodes and edges
      const nodeIds: string[] = []
      for (const node of value._nodes) {
        nodeIds.push(addNode(node))
      }
      for (let i = 0; i < value._relationships.length; i++) {
        addEdge(nodeIds[i], nodeIds[i + 1], value._relationships[i])
      }
    } else if (Array.isArray(value)) {
      for (const item of value) {
        processValue(item)
      }
    }
  }

  // Process all cells in all rows
  for (const row of rows) {
    for (const cell of row) {
      processValue(cell)
    }
  }

  return {
    nodes: Array.from(nodesMap.values()),
    edges: Array.from(edgesMap.values()),
  }
}

// ============================================================================
// Node Label Colors
// ============================================================================

/** Color palette for different node labels */
const LABEL_COLORS: Record<string, { bg: string; border: string; text: string }> = {
  Device: { bg: '#7c3aed', border: '#5b21b6', text: '#ffffff' },
  Link: { bg: '#2563eb', border: '#1d4ed8', text: '#ffffff' },
  Metro: { bg: '#0891b2', border: '#0e7490', text: '#ffffff' },
  Validator: { bg: '#16a34a', border: '#15803d', text: '#ffffff' },
  Interface: { bg: '#ea580c', border: '#c2410c', text: '#ffffff' },
  default: { bg: '#6b7280', border: '#4b5563', text: '#ffffff' },
}

/** Get color scheme for a node label */
export function getLabelColor(label: string): { bg: string; border: string; text: string } {
  return LABEL_COLORS[label] || LABEL_COLORS.default
}
