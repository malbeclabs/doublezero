import { useEffect, useRef, useCallback, useState } from 'react'
import cytoscape from 'cytoscape'
import type { Core, NodeSingular, EdgeSingular } from 'cytoscape'
import { ZoomIn, ZoomOut, Maximize } from 'lucide-react'
import type { GraphData, GraphNode, GraphEdge } from '@/lib/neo4j-utils'
import { getLabelColor } from '@/lib/neo4j-utils'
import { useTheme } from '@/hooks/use-theme'

interface CypherGraphProps {
  data: GraphData
}

export function CypherGraph({ data }: CypherGraphProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const cyRef = useRef<Core | null>(null)
  const { theme } = useTheme()
  const isDark = theme === 'dark'

  const [hoveredNode, setHoveredNode] = useState<{
    node: GraphNode
    x: number
    y: number
  } | null>(null)

  const [hoveredEdge, setHoveredEdge] = useState<{
    edge: GraphEdge
    x: number
    y: number
  } | null>(null)

  // Get node color based on primary label
  const getNodeColor = useCallback((labels: string[]) => {
    const primaryLabel = labels.find(l => !l.startsWith('_')) || labels[0] || 'default'
    return getLabelColor(primaryLabel)
  }, [])

  // Initialize Cytoscape
  useEffect(() => {
    if (!containerRef.current || data.nodes.length === 0) return

    // Convert GraphData to Cytoscape format
    const elements = [
      ...data.nodes.map(node => {
        const colors = getNodeColor(node.labels)
        return {
          data: {
            id: node.id,
            label: node.displayName,
            labels: node.labels,
            properties: node.properties,
          },
          style: {
            'background-color': colors.bg,
            'border-color': colors.border,
            color: colors.text,
          },
        }
      }),
      ...data.edges.map(edge => ({
        data: {
          id: edge.id,
          source: edge.source,
          target: edge.target,
          label: edge.type,
          type: edge.type,
          properties: edge.properties,
        },
      })),
    ]

    const cy = cytoscape({
      container: containerRef.current,
      elements,
      style: [
        {
          selector: 'node',
          style: {
            label: 'data(label)',
            'text-valign': 'center',
            'text-halign': 'center',
            'font-size': '10px',
            'font-weight': 500,
            width: 60,
            height: 60,
            'border-width': 2,
            'text-wrap': 'ellipsis',
            'text-max-width': '55px',
          },
        },
        {
          selector: 'edge',
          style: {
            width: 2,
            'line-color': isDark ? '#6b7280' : '#9ca3af',
            'target-arrow-color': isDark ? '#6b7280' : '#9ca3af',
            'target-arrow-shape': 'triangle',
            'curve-style': 'bezier',
            label: 'data(label)',
            'font-size': '8px',
            'text-rotation': 'autorotate',
            'text-margin-y': -8,
            color: isDark ? '#9ca3af' : '#6b7280',
          },
        },
        {
          selector: 'node:selected',
          style: {
            'border-width': 4,
            'border-color': isDark ? '#f97316' : '#ea580c',
          },
        },
        {
          selector: 'edge:selected',
          style: {
            width: 4,
            'line-color': isDark ? '#f97316' : '#ea580c',
            'target-arrow-color': isDark ? '#f97316' : '#ea580c',
          },
        },
      ],
      layout: {
        name: 'cose',
        animate: false,
        nodeDimensionsIncludeLabels: true,
        idealEdgeLength: () => 100,
        nodeRepulsion: () => 8000,
        nodeOverlap: 20,
        gravity: 0.25,
        numIter: 1000,
        coolingFactor: 0.95,
        minTemp: 1.0,
      },
      minZoom: 0.2,
      maxZoom: 3,
      wheelSensitivity: 0.3,
    })

    cyRef.current = cy

    // Event handlers for tooltips
    cy.on('mouseover', 'node', (evt) => {
      const node = evt.target as NodeSingular
      const pos = node.renderedPosition()
      const container = containerRef.current?.getBoundingClientRect()
      if (container) {
        setHoveredNode({
          node: {
            id: node.data('id'),
            labels: node.data('labels'),
            properties: node.data('properties'),
            displayName: node.data('label'),
          },
          x: container.left + pos.x,
          y: container.top + pos.y,
        })
      }
    })

    cy.on('mouseout', 'node', () => {
      setHoveredNode(null)
    })

    cy.on('mouseover', 'edge', (evt) => {
      const edge = evt.target as EdgeSingular
      const midpoint = edge.renderedMidpoint()
      const container = containerRef.current?.getBoundingClientRect()
      if (container) {
        setHoveredEdge({
          edge: {
            id: edge.data('id'),
            source: edge.data('source'),
            target: edge.data('target'),
            type: edge.data('type'),
            properties: edge.data('properties'),
          },
          x: container.left + midpoint.x,
          y: container.top + midpoint.y,
        })
      }
    })

    cy.on('mouseout', 'edge', () => {
      setHoveredEdge(null)
    })

    return () => {
      cy.destroy()
      cyRef.current = null
    }
  }, [data, isDark, getNodeColor])

  const handleZoomIn = () => {
    cyRef.current?.zoom(cyRef.current.zoom() * 1.3)
  }

  const handleZoomOut = () => {
    cyRef.current?.zoom(cyRef.current.zoom() / 1.3)
  }

  const handleFit = () => {
    cyRef.current?.fit(undefined, 30)
  }

  if (data.nodes.length === 0) {
    return (
      <div className="flex items-center justify-center h-[400px] text-muted-foreground">
        No graph data to display
      </div>
    )
  }

  return (
    <div className="relative">
      {/* Graph container */}
      <div
        ref={containerRef}
        className="w-full h-[400px] bg-muted/30 rounded-lg"
      />

      {/* Zoom controls */}
      <div className="absolute top-2 right-2 flex flex-col gap-1">
        <button
          onClick={handleZoomIn}
          className="p-1.5 rounded bg-card border border-border hover:bg-muted transition-colors"
          title="Zoom in"
        >
          <ZoomIn className="w-4 h-4" />
        </button>
        <button
          onClick={handleZoomOut}
          className="p-1.5 rounded bg-card border border-border hover:bg-muted transition-colors"
          title="Zoom out"
        >
          <ZoomOut className="w-4 h-4" />
        </button>
        <button
          onClick={handleFit}
          className="p-1.5 rounded bg-card border border-border hover:bg-muted transition-colors"
          title="Fit to view"
        >
          <Maximize className="w-4 h-4" />
        </button>
      </div>

      {/* Node count indicator */}
      <div className="absolute bottom-2 left-2 text-xs text-muted-foreground bg-card/80 px-2 py-1 rounded border border-border">
        {data.nodes.length} nodes, {data.edges.length} edges
      </div>

      {/* Node tooltip */}
      {hoveredNode && (
        <div
          className="fixed z-50 bg-popover border border-border rounded-lg shadow-lg p-3 text-sm max-w-xs pointer-events-none"
          style={{
            left: hoveredNode.x + 10,
            top: hoveredNode.y - 10,
            transform: 'translateY(-100%)',
          }}
        >
          <div className="font-medium mb-1">
            {hoveredNode.node.labels.join(', ')}
          </div>
          <div className="text-muted-foreground text-xs mb-2">
            {hoveredNode.node.displayName}
          </div>
          <div className="space-y-0.5 text-xs">
            {Object.entries(hoveredNode.node.properties).slice(0, 5).map(([key, value]) => (
              <div key={key} className="flex gap-2">
                <span className="text-muted-foreground">{key}:</span>
                <span className="truncate">{String(value)}</span>
              </div>
            ))}
            {Object.keys(hoveredNode.node.properties).length > 5 && (
              <div className="text-muted-foreground italic">
                +{Object.keys(hoveredNode.node.properties).length - 5} more
              </div>
            )}
          </div>
        </div>
      )}

      {/* Edge tooltip */}
      {hoveredEdge && (
        <div
          className="fixed z-50 bg-popover border border-border rounded-lg shadow-lg p-3 text-sm max-w-xs pointer-events-none"
          style={{
            left: hoveredEdge.x + 10,
            top: hoveredEdge.y - 10,
            transform: 'translateY(-100%)',
          }}
        >
          <div className="font-medium mb-1">
            [:{hoveredEdge.edge.type}]
          </div>
          {Object.keys(hoveredEdge.edge.properties).length > 0 && (
            <div className="space-y-0.5 text-xs">
              {Object.entries(hoveredEdge.edge.properties).slice(0, 5).map(([key, value]) => (
                <div key={key} className="flex gap-2">
                  <span className="text-muted-foreground">{key}:</span>
                  <span className="truncate">{String(value)}</span>
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
