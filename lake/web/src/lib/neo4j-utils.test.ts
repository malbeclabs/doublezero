import { describe, it, expect } from 'vitest'
import {
  isNeo4jNode,
  isNeo4jRelationship,
  isNeo4jPath,
  isPathArray,
  isNeo4jValue,
  formatNeo4jNode,
  formatNeo4jRelationship,
  formatNeo4jPath,
  formatNeo4jValue,
  formatPathArray,
  containsGraphData,
  extractGraphData,
  getLabelColor,
  type Neo4jNode,
  type Neo4jRelationship,
  type Neo4jPath,
} from './neo4j-utils'

// Test data factories
function createNode(labels: string[], properties: Record<string, unknown>): Neo4jNode {
  return { _labels: labels, _properties: properties }
}

function createRelationship(type: string, properties: Record<string, unknown> = {}): Neo4jRelationship {
  return { _type: type, _properties: properties }
}

function createPath(nodes: Neo4jNode[], relationships: Neo4jRelationship[]): Neo4jPath {
  return { _nodes: nodes, _relationships: relationships }
}

describe('Type Guards', () => {
  describe('isNeo4jNode', () => {
    it('returns true for valid nodes', () => {
      expect(isNeo4jNode({ _labels: ['Device'], _properties: { code: 'NYC-01' } })).toBe(true)
      expect(isNeo4jNode({ _labels: [], _properties: {} })).toBe(true)
    })

    it('returns false for invalid values', () => {
      expect(isNeo4jNode(null)).toBe(false)
      expect(isNeo4jNode(undefined)).toBe(false)
      expect(isNeo4jNode({})).toBe(false)
      expect(isNeo4jNode({ _labels: 'Device' })).toBe(false)
      expect(isNeo4jNode({ _labels: ['Device'] })).toBe(false)
      expect(isNeo4jNode({ _properties: {} })).toBe(false)
      expect(isNeo4jNode('string')).toBe(false)
      expect(isNeo4jNode(123)).toBe(false)
    })
  })

  describe('isNeo4jRelationship', () => {
    it('returns true for valid relationships', () => {
      expect(isNeo4jRelationship({ _type: 'CONNECTS', _properties: {} })).toBe(true)
      expect(isNeo4jRelationship({ _type: 'ISIS_ADJACENT', _properties: { metric: 100 } })).toBe(true)
    })

    it('returns false for invalid values', () => {
      expect(isNeo4jRelationship(null)).toBe(false)
      expect(isNeo4jRelationship(undefined)).toBe(false)
      expect(isNeo4jRelationship({})).toBe(false)
      expect(isNeo4jRelationship({ _type: 123 })).toBe(false)
      expect(isNeo4jRelationship({ _type: 'CONNECTS' })).toBe(false)
      expect(isNeo4jRelationship({ _properties: {} })).toBe(false)
    })
  })

  describe('isNeo4jPath', () => {
    it('returns true for valid paths', () => {
      const node1 = createNode(['Device'], { code: 'A' })
      const node2 = createNode(['Device'], { code: 'B' })
      const rel = createRelationship('CONNECTS')
      expect(isNeo4jPath({ _nodes: [node1, node2], _relationships: [rel] })).toBe(true)
      expect(isNeo4jPath({ _nodes: [node1], _relationships: [] })).toBe(true)
    })

    it('returns false for invalid values', () => {
      expect(isNeo4jPath(null)).toBe(false)
      expect(isNeo4jPath(undefined)).toBe(false)
      expect(isNeo4jPath({})).toBe(false)
      expect(isNeo4jPath({ _nodes: [], _relationships: 'not array' })).toBe(false)
      expect(isNeo4jPath({ _nodes: 'not array', _relationships: [] })).toBe(false)
      expect(isNeo4jPath({ _nodes: [{ invalid: true }], _relationships: [] })).toBe(false)
    })
  })

  describe('isPathArray', () => {
    it('returns true for valid path arrays', () => {
      const pathArray = [
        { type: 'device', code: 'NYC-01' },
        { type: 'link', code: 'L1' },
        { type: 'device', code: 'LAX-01' },
      ]
      expect(isPathArray(pathArray)).toBe(true)
    })

    it('returns false for arrays too short', () => {
      expect(isPathArray([{ type: 'device', code: 'A' }])).toBe(false)
      expect(isPathArray([])).toBe(false)
    })

    it('returns false for invalid elements', () => {
      expect(isPathArray([{ type: 'device' }, { type: 'link' }])).toBe(false)
      expect(isPathArray([{ code: 'A' }, { code: 'B' }])).toBe(false)
      expect(isPathArray([{ type: 'invalid', code: 'A' }, { type: 'device', code: 'B' }])).toBe(false)
    })

    it('returns false for non-arrays', () => {
      expect(isPathArray('string')).toBe(false)
      expect(isPathArray(123)).toBe(false)
      expect(isPathArray(null)).toBe(false)
    })
  })

  describe('isNeo4jValue', () => {
    it('returns true for any Neo4j type', () => {
      expect(isNeo4jValue(createNode(['Device'], {}))).toBe(true)
      expect(isNeo4jValue(createRelationship('CONNECTS'))).toBe(true)
      expect(isNeo4jValue(createPath([createNode(['A'], {})], []))).toBe(true)
      expect(isNeo4jValue([{ type: 'device', code: 'A' }, { type: 'link', code: 'B' }])).toBe(true)
    })

    it('returns false for regular values', () => {
      expect(isNeo4jValue('string')).toBe(false)
      expect(isNeo4jValue(123)).toBe(false)
      expect(isNeo4jValue({ regular: 'object' })).toBe(false)
    })
  })
})

describe('Display Formatters', () => {
  describe('formatNeo4jNode', () => {
    it('formats node with code property', () => {
      const node = createNode(['Device'], { code: 'NYC-CORE-01', pk: 'abc123' })
      expect(formatNeo4jNode(node)).toBe('Device: NYC-CORE-01')
    })

    it('formats node with name property when no code', () => {
      const node = createNode(['Metro'], { name: 'New York', pk: 'nyc' })
      expect(formatNeo4jNode(node)).toBe('Metro: New York')
    })

    it('formats node with pk property when no code/name', () => {
      const node = createNode(['Link'], { pk: 'link-123' })
      expect(formatNeo4jNode(node)).toBe('Link: link-123')
    })

    it('uses first property when no identifier', () => {
      const node = createNode(['Custom'], { foo: 'bar' })
      expect(formatNeo4jNode(node)).toBe('Custom: bar')
    })

    it('returns ? for node with no properties', () => {
      const node = createNode(['Empty'], {})
      expect(formatNeo4jNode(node)).toBe('Empty: ?')
    })

    it('uses Node as label when no labels', () => {
      const node = createNode([], { code: 'test' })
      expect(formatNeo4jNode(node)).toBe('Node: test')
    })

    it('skips internal labels starting with _', () => {
      const node = createNode(['_Internal', 'Device'], { code: 'A' })
      expect(formatNeo4jNode(node)).toBe('Device: A')
    })
  })

  describe('formatNeo4jRelationship', () => {
    it('formats relationship with no properties', () => {
      const rel = createRelationship('CONNECTS')
      expect(formatNeo4jRelationship(rel)).toBe('[:CONNECTS]')
    })

    it('formats relationship with metric property', () => {
      const rel = createRelationship('ISIS_ADJACENT', { metric: 100 })
      expect(formatNeo4jRelationship(rel)).toBe('[:ISIS_ADJACENT {metric: 100}]')
    })

    it('formats relationship with multiple properties', () => {
      const rel = createRelationship('LINK', { metric: 50, weight: 10 })
      expect(formatNeo4jRelationship(rel)).toBe('[:LINK {metric: 50, weight: 10}]')
    })

    it('limits to 2 properties', () => {
      const rel = createRelationship('COMPLEX', { a: 1, b: 2, c: 3, d: 4 })
      const formatted = formatNeo4jRelationship(rel)
      const propMatches = formatted.match(/\w+:/g)
      expect(propMatches?.length).toBeLessThanOrEqual(2)
    })
  })

  describe('formatNeo4jPath', () => {
    it('formats simple path', () => {
      const node1 = createNode(['Device'], { code: 'A' })
      const node2 = createNode(['Device'], { code: 'B' })
      const rel = createRelationship('CONNECTS')
      const path = createPath([node1, node2], [rel])

      expect(formatNeo4jPath(path)).toBe('Device: A -[:CONNECTS]-> Device: B')
    })

    it('formats longer path', () => {
      const nodes = [
        createNode(['Device'], { code: 'A' }),
        createNode(['Device'], { code: 'B' }),
        createNode(['Device'], { code: 'C' }),
      ]
      const rels = [
        createRelationship('R1'),
        createRelationship('R2'),
      ]
      const path = createPath(nodes, rels)

      expect(formatNeo4jPath(path)).toBe('Device: A -[:R1]-> Device: B -[:R2]-> Device: C')
    })

    it('formats single node path', () => {
      const node = createNode(['Device'], { code: 'A' })
      const path = createPath([node], [])

      expect(formatNeo4jPath(path)).toBe('Device: A')
    })
  })

  describe('formatPathArray', () => {
    it('formats path array as arrow notation', () => {
      const arr = [
        { type: 'device' as const, code: 'NYC' },
        { type: 'link' as const, code: 'L1' },
        { type: 'device' as const, code: 'LAX' },
      ]
      expect(formatPathArray(arr)).toBe('NYC → LAX')
    })

    it('handles multiple hops', () => {
      const arr = [
        { type: 'device' as const, code: 'A' },
        { type: 'link' as const, code: 'L1' },
        { type: 'device' as const, code: 'B' },
        { type: 'link' as const, code: 'L2' },
        { type: 'device' as const, code: 'C' },
      ]
      expect(formatPathArray(arr)).toBe('A → B → C')
    })
  })

  describe('formatNeo4jValue', () => {
    it('formats null/undefined as empty string', () => {
      expect(formatNeo4jValue(null)).toBe('')
      expect(formatNeo4jValue(undefined)).toBe('')
    })

    it('formats Neo4j nodes', () => {
      const node = createNode(['Device'], { code: 'A' })
      expect(formatNeo4jValue(node)).toBe('Device: A')
    })

    it('formats Neo4j relationships', () => {
      const rel = createRelationship('CONNECTS')
      expect(formatNeo4jValue(rel)).toBe('[:CONNECTS]')
    })

    it('formats Neo4j paths', () => {
      const path = createPath(
        [createNode(['D'], { code: 'A' }), createNode(['D'], { code: 'B' })],
        [createRelationship('R')]
      )
      expect(formatNeo4jValue(path)).toBe('D: A -[:R]-> D: B')
    })

    it('formats path arrays', () => {
      const arr = [
        { type: 'device' as const, code: 'X' },
        { type: 'link' as const, code: 'L' },
        { type: 'device' as const, code: 'Y' },
      ]
      expect(formatNeo4jValue(arr)).toBe('X → Y')
    })

    it('formats arrays of Neo4j values', () => {
      const nodes = [
        createNode(['D'], { code: 'A' }),
        createNode(['D'], { code: 'B' }),
      ]
      expect(formatNeo4jValue(nodes)).toBe('[D: A, D: B]')
    })

    it('formats primitive values', () => {
      expect(formatNeo4jValue('hello')).toBe('hello')
      expect(formatNeo4jValue(123)).toBe('123')
      expect(formatNeo4jValue(true)).toBe('true')
    })

    it('truncates long objects', () => {
      const obj = { a: 'very long string', b: 'another long string', c: 'more content' }
      const result = formatNeo4jValue(obj)
      expect(result.length).toBeLessThanOrEqual(50)
      expect(result).toContain('...')
    })
  })
})

describe('Graph Data Extraction', () => {
  describe('containsGraphData', () => {
    it('returns true for rows with nodes', () => {
      const rows = [[createNode(['Device'], { code: 'A' })]]
      expect(containsGraphData(rows)).toBe(true)
    })

    it('returns true for rows with relationships', () => {
      const rows = [[createRelationship('CONNECTS')]]
      expect(containsGraphData(rows)).toBe(true)
    })

    it('returns true for rows with paths', () => {
      const path = createPath([createNode(['D'], {})], [])
      const rows = [[path]]
      expect(containsGraphData(rows)).toBe(true)
    })

    it('returns true for rows with path arrays', () => {
      const pathArr = [
        { type: 'device', code: 'A' },
        { type: 'link', code: 'L' },
        { type: 'device', code: 'B' },
      ]
      const rows = [[pathArr]]
      expect(containsGraphData(rows)).toBe(true)
    })

    it('returns true for nested arrays with graph data', () => {
      const rows = [[[createNode(['D'], { code: 'A' })]]]
      expect(containsGraphData(rows)).toBe(true)
    })

    it('returns false for rows without graph data', () => {
      expect(containsGraphData([[]])).toBe(false)
      expect(containsGraphData([['string', 123]])).toBe(false)
      expect(containsGraphData([[{ regular: 'object' }]])).toBe(false)
    })
  })

  describe('extractGraphData', () => {
    it('extracts nodes from results', () => {
      const node = createNode(['Device'], { code: 'NYC-01' })
      const result = extractGraphData([[node]])

      expect(result.nodes).toHaveLength(1)
      expect(result.nodes[0].labels).toEqual(['Device'])
      expect(result.nodes[0].displayName).toBe('NYC-01')
      expect(result.edges).toHaveLength(0)
    })

    it('deduplicates nodes by id', () => {
      const node = createNode(['Device'], { pk: 'abc123' })
      const result = extractGraphData([[node], [node], [node]])

      expect(result.nodes).toHaveLength(1)
    })

    it('extracts paths with nodes and edges', () => {
      const node1 = createNode(['Device'], { pk: 'a' })
      const node2 = createNode(['Device'], { pk: 'b' })
      const rel = createRelationship('CONNECTS', { metric: 100 })
      const path = createPath([node1, node2], [rel])

      const result = extractGraphData([[path]])

      expect(result.nodes).toHaveLength(2)
      expect(result.edges).toHaveLength(1)
      expect(result.edges[0].type).toBe('CONNECTS')
      expect(result.edges[0].properties).toEqual({ metric: 100 })
    })

    it('handles nested arrays', () => {
      const nodes = [
        createNode(['Device'], { pk: 'a' }),
        createNode(['Device'], { pk: 'b' }),
      ]
      const result = extractGraphData([[nodes]])

      expect(result.nodes).toHaveLength(2)
    })

    it('returns empty graph for non-graph data', () => {
      const result = extractGraphData([['string', 123]])

      expect(result.nodes).toHaveLength(0)
      expect(result.edges).toHaveLength(0)
    })
  })
})

describe('Label Colors', () => {
  describe('getLabelColor', () => {
    it('returns colors for known labels', () => {
      const deviceColor = getLabelColor('Device')
      expect(deviceColor.bg).toBe('#7c3aed')

      const linkColor = getLabelColor('Link')
      expect(linkColor.bg).toBe('#2563eb')

      const metroColor = getLabelColor('Metro')
      expect(metroColor.bg).toBe('#0891b2')
    })

    it('returns default color for unknown labels', () => {
      const unknownColor = getLabelColor('UnknownLabel')
      expect(unknownColor.bg).toBe('#6b7280')
    })

    it('returns all required color properties', () => {
      const color = getLabelColor('Device')
      expect(color).toHaveProperty('bg')
      expect(color).toHaveProperty('border')
      expect(color).toHaveProperty('text')
    })
  })
})
