/**
 * Topology View Integration Hook
 *
 * This hook bridges the unified TopologyContext with view-specific state management.
 * It allows views to gradually adopt the unified system while maintaining their
 * existing state management patterns.
 *
 * Usage:
 *
 * 1. Wrap your view in TopologyProvider (done in topology-page.tsx)
 * 2. Use this hook to access context state and sync with local state
 *
 * ```tsx
 * function TopologyMap() {
 *   const {
 *     mode, setMode,
 *     selection, setSelection,
 *     panel, openPanel, closePanel,
 *     overlays, toggleOverlay,
 *   } = useTopologyView()
 *
 *   // Use mode instead of local pathModeEnabled, etc.
 *   const isPathMode = mode === 'path'
 *
 *   // Use overlays instead of local showValidators, stakeOverlayMode, etc.
 *   const showValidators = overlays.validators
 *   const showCriticality = overlays.criticality
 * }
 * ```
 */

import { useCallback } from 'react'
import { useTopology, type TopologyMode, type Selection } from './TopologyContext'
import type { DeviceInfo, LinkInfo, MetroInfo, ValidatorInfo, SelectedItemData } from './types'

export interface TopologyViewState {
  // Mode management
  mode: TopologyMode
  setMode: (mode: TopologyMode) => void
  isExploreMode: boolean
  isPathMode: boolean
  isWhatIfRemovalMode: boolean
  isWhatIfAdditionMode: boolean
  isImpactMode: boolean
  isInAnalysisMode: boolean

  // Selection management
  selection: Selection | null
  setSelection: (selection: Selection | null) => void
  selectedDevicePK: string | null
  selectedLinkPK: string | null
  selectedMetroPK: string | null
  selectedValidatorPK: string | null

  // Panel management
  isPanelOpen: boolean
  panelWidth: number
  panelContent: 'details' | 'mode' | 'overlay'
  openPanel: (content: 'details' | 'mode' | 'overlay') => void
  closePanel: () => void
  setPanelWidth: (width: number) => void
  openDetailsPanel: () => void
  openModePanel: () => void

  // Overlay toggles
  showValidators: boolean
  showStakeOverlay: boolean
  showLinkHealth: boolean
  showTrafficFlow: boolean
  showMetroClustering: boolean
  showCriticality: boolean
  showIsisHealth: boolean
  toggleValidators: () => void
  toggleStakeOverlay: () => void
  toggleLinkHealth: () => void
  toggleTrafficFlow: () => void
  toggleMetroClustering: () => void
  toggleCriticality: () => void
  toggleIsisHealth: () => void

  // View type
  view: 'map' | 'graph'
}

export function useTopologyView(): TopologyViewState {
  const {
    mode,
    setMode,
    selection,
    setSelection,
    panel,
    openPanel,
    closePanel,
    setPanelWidth,
    overlays,
    toggleOverlay,
    view,
  } = useTopology()

  // Mode convenience helpers
  const isExploreMode = mode === 'explore'
  const isPathMode = mode === 'path'
  const isWhatIfRemovalMode = mode === 'whatif-removal'
  const isWhatIfAdditionMode = mode === 'whatif-addition'
  const isImpactMode = mode === 'impact'
  const isInAnalysisMode = !isExploreMode

  // Selection convenience helpers
  const selectedDevicePK = selection?.type === 'device' ? selection.id : null
  const selectedLinkPK = selection?.type === 'link' ? selection.id : null
  const selectedMetroPK = selection?.type === 'metro' ? selection.id : null
  const selectedValidatorPK = selection?.type === 'validator' ? selection.id : null

  // Panel convenience helpers
  const isPanelOpen = panel.isOpen
  const panelWidth = panel.width
  const panelContent = panel.content

  const openDetailsPanel = useCallback(() => openPanel('details'), [openPanel])
  const openModePanel = useCallback(() => openPanel('mode'), [openPanel])

  // Overlay convenience helpers
  const showValidators = overlays.validators
  const showStakeOverlay = overlays.stake
  const showLinkHealth = overlays.linkHealth
  const showTrafficFlow = overlays.trafficFlow
  const showMetroClustering = overlays.metroClustering
  const showCriticality = overlays.criticality
  const showIsisHealth = overlays.isisHealth

  const toggleValidators = useCallback(() => toggleOverlay('validators'), [toggleOverlay])
  const toggleStakeOverlay = useCallback(() => toggleOverlay('stake'), [toggleOverlay])
  const toggleLinkHealth = useCallback(() => toggleOverlay('linkHealth'), [toggleOverlay])
  const toggleTrafficFlow = useCallback(() => toggleOverlay('trafficFlow'), [toggleOverlay])
  const toggleMetroClustering = useCallback(() => toggleOverlay('metroClustering'), [toggleOverlay])
  const toggleCriticality = useCallback(() => toggleOverlay('criticality'), [toggleOverlay])
  const toggleIsisHealth = useCallback(() => toggleOverlay('isisHealth'), [toggleOverlay])

  return {
    mode,
    setMode,
    isExploreMode,
    isPathMode,
    isWhatIfRemovalMode,
    isWhatIfAdditionMode,
    isImpactMode,
    isInAnalysisMode,

    selection,
    setSelection,
    selectedDevicePK,
    selectedLinkPK,
    selectedMetroPK,
    selectedValidatorPK,

    isPanelOpen,
    panelWidth,
    panelContent,
    openPanel,
    closePanel,
    setPanelWidth,
    openDetailsPanel,
    openModePanel,

    showValidators,
    showStakeOverlay,
    showLinkHealth,
    showTrafficFlow,
    showMetroClustering,
    showCriticality,
    showIsisHealth,
    toggleValidators,
    toggleStakeOverlay,
    toggleLinkHealth,
    toggleTrafficFlow,
    toggleMetroClustering,
    toggleCriticality,
    toggleIsisHealth,

    view,
  }
}

/**
 * Convert SelectedItemData to Selection format
 */
export function toSelection(item: SelectedItemData): Selection {
  if (item.type === 'validator') {
    return { type: 'validator', id: item.data.votePubkey }
  }
  return { type: item.type, id: item.data.pk }
}

/**
 * Create SelectedItemData from entity info
 */
export function createDeviceSelection(device: DeviceInfo): SelectedItemData {
  return { type: 'device', data: device }
}

export function createLinkSelection(link: LinkInfo): SelectedItemData {
  return { type: 'link', data: link }
}

export function createMetroSelection(metro: MetroInfo): SelectedItemData {
  return { type: 'metro', data: metro }
}

export function createValidatorSelection(validator: ValidatorInfo): SelectedItemData {
  return { type: 'validator', data: validator }
}
