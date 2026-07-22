<template>
  <div class="memory-graph">
    <!-- Toolbar -->
    <div class="graph-toolbar">
      <div class="toolbar-left">
        <span class="toolbar-label">{{ t('memory.graph.depth') }}:</span>
        <t-radio-group
          :value="depth"
          variant="default"
          size="small"
          @change="(val: number) => { depth = val; applyDepthFilter() }"
        >
          <t-radio-button :value="1">1</t-radio-button>
          <t-radio-button :value="2">2</t-radio-button>
          <t-radio-button :value="3">3</t-radio-button>
        </t-radio-group>
      </div>
      <div class="toolbar-right">
        <t-button
          variant="text"
          size="small"
          :title="t('memory.graph.fitToScreen')"
          @click="fitToScreen"
        >
          <t-icon name="fullscreen" />
        </t-button>
        <t-button
          variant="text"
          size="small"
          :title="t('memory.graph.centerGraph')"
          @click="recenterGraph"
        >
          <t-icon name="focus" />
        </t-button>
      </div>
    </div>

    <!-- Graph canvas -->
    <div class="graph-canvas-wrapper">
      <!-- Loading state -->
      <div v-if="memoryStore.graphLoading" class="loading-container">
        <t-loading :text="t('memory.graph.loading')" size="large" />
      </div>

      <!-- Empty state (no data) -->
      <div
        v-else-if="!memoryStore.graphData || !graphData?.nodes?.length"
        class="empty-state"
      >
        <t-icon name="chart-bubble" size="48px" class="empty-icon" />
        <span class="empty-title">{{ t('memory.graph.emptyTitle') }}</span>
        <span class="empty-desc">
          {{ t('memory.graph.emptyDesc') }}
        </span>
      </div>

      <!-- SVG container -->
      <div
        v-else
        ref="svgContainerRef"
        class="graph-svg-container"
        @contextmenu.prevent
      >
        <svg
          ref="svgRef"
          class="graph-svg"
          :viewBox="`0 0 ${svgWidth} ${svgHeight}`"
        >
          <!-- Definitions -->
          <defs>
            <!-- Drop shadow filter for nodes -->
            <filter id="graph-node-shadow" x="-20%" y="-20%" width="140%" height="140%">
              <feDropShadow dx="0" dy="1" stdDeviation="2" flood-opacity="0.15" />
            </filter>
            <!-- Glow for decision ring -->
            <filter id="graph-glow" x="-50%" y="-50%" width="200%" height="200%">
              <feGaussianBlur stdDeviation="2" result="blur" />
              <feMerge>
                <feMergeNode in="blur" />
                <feMergeNode in="SourceGraphic" />
              </feMerge>
            </filter>
          </defs>

          <!-- Pan/zoom root group -->
          <g ref="rootGRef" class="graph-root">
            <!-- Background rect for canvas-wide events -->
            <rect
              :width="svgWidth"
              :height="svgHeight"
              fill="transparent"
              class="graph-bg"
            />

            <!-- Edges group (below nodes) -->
            <g class="graph-edges">
              <line
                v-for="(edge, idx) in displayEdges"
                :key="'edge-' + idx"
                :x1="npx(edge.sx)"
                :y1="npx(edge.sy)"
                :x2="npx(edge.tx)"
                :y2="npx(edge.ty)"
                :class="['graph-edge', `edge-${edge.relation}`]"
                :stroke="edgeColor(edge.relation)"
                :stroke-width="npx(edgeWeight(edge))"
                :stroke-dasharray="edgeDash(edge.relation)"
                :opacity="edgeOpacity"
              />
            </g>

            <!-- Nodes group (above edges) -->
            <g class="graph-nodes">
              <g
                v-for="(node, idx) in displayNodes"
                :key="'node-' + idx"
                :data-node-id="node.id"
                :class="['graph-node-group', { 'node-dragging': draggingNodeId === node.id }]"
                :transform="`translate(${npx(node.x)},${npx(node.y)})`"
                :style="{ cursor: 'pointer' }"
                @mousedown.prevent="onNodeMouseDown($event, node)"
                @click.stop="onNodeClick(node)"
                @mouseenter="onNodeHover(node)"
                @mouseleave="onNodeBlur"
              >
                <!-- Decision ring -->
                <circle
                  v-if="node.verdict === 'decision'"
                  :r="npx(nodeRadius(node) + 5)"
                  fill="none"
                  stroke="#0052d9"
                  :stroke-width="npx(3)"
                  :opacity="0.6"
                  filter="url(#graph-glow)"
                />

                <!-- Hub ring -->
                <circle
                  v-if="node.hub_score > 1.0"
                  :r="npx(nodeRadius(node) + 3)"
                  fill="none"
                  stroke="#ffa940"
                  :stroke-width="npx(2.5)"
                  :stroke-dasharray="npx(4) + ' ' + npx(3)"
                  :opacity="0.7"
                />

                <!-- Main circle -->
                <circle
                  :r="npx(nodeRadius(node))"
                  :fill="nodeColor(node)"
                  :stroke="hoveredNodeId === node.id ? '#fff' : nodeBorder(node)"
                  :stroke-width="npx(hoveredNodeId === node.id ? 2.5 : 1.5)"
                  :opacity="nodeOpacity(node)"
                  class="graph-node-circle"
                  filter="url(#graph-node-shadow)"
                />

                <!-- Stale indicator (small dot) -->
                <circle
                  v-if="node.is_stale"
                  :cx="npx(nodeRadius(node) - 3)"
                  :cy="npx(-nodeRadius(node) + 3)"
                  :r="npx(3)"
                  fill="#faad14"
                  stroke="#fff"
                  :stroke-width="npx(1)"
                />

                <!-- Label -->
                <text
                  :x="npx(0)"
                  :y="npx(nodeRadius(node) + 14)"
                  text-anchor="middle"
                  class="graph-node-label"
                  :font-size="npx(nodeLabelFontSize(node))"
                  :opacity="labelOpacity(node)"
                >
                  {{ node.label }}
                </text>
              </g>
            </g>
          </g>
        </svg>

        <!-- Legend overlay -->
        <div class="graph-legend" :class="{ collapsed: legendCollapsed }">
          <div class="legend-header" @click="legendCollapsed = !legendCollapsed">
            <span>{{ t('memory.graph.legend') }}</span>
            <t-icon :name="legendCollapsed ? 'chevron-up' : 'chevron-down'" size="14px" />
          </div>
          <div v-show="!legendCollapsed" class="legend-body">
            <div class="legend-section">
              <div class="legend-section-title">{{ t('memory.graph.nodeTypes') }}</div>
              <div
                v-for="item in typeLegendItems"
                :key="item.key"
                class="legend-item"
              >
                <span class="legend-dot" :style="{ background: item.color }" />
                <span class="legend-label">{{ item.label }}</span>
              </div>
            </div>
            <div class="legend-section">
              <div class="legend-section-title">{{ t('memory.graph.verdict') }}</div>
              <div class="legend-item">
                <span class="legend-ring-demo" />
                <span class="legend-label">{{ t('memory.graph.decisionRing') }}</span>
              </div>
              <div class="legend-item">
                <span class="legend-dot-dimmed" />
                <span class="legend-label">{{ t('memory.graph.refutedDimmed') }}</span>
              </div>
              <div class="legend-item">
                <span class="legend-dot-hub" />
                <span class="legend-label">{{ t('memory.graph.hubScore') }}</span>
              </div>
            </div>
            <div class="legend-section">
              <div class="legend-section-title">{{ t('memory.graph.relations') }}</div>
              <div
                v-for="item in relationLegendItems"
                :key="item.key"
                class="legend-item"
              >
                <span class="legend-line" :class="item.cssClass" :style="{ background: item.color }" />
                <span class="legend-label">{{ item.label }}</span>
              </div>
            </div>
            <div class="legend-section">
              <div class="legend-section-title">{{ t('memory.graph.interaction') }}</div>
              <div class="legend-item">
                <span class="legend-label legend-hint">{{ t('memory.graph.clickNodeHint') }}</span>
              </div>
              <div class="legend-item">
                <span class="legend-label legend-hint">{{ t('memory.graph.dragNodeHint') }}</span>
              </div>
              <div class="legend-item">
                <span class="legend-label legend-hint">{{ t('memory.graph.scrollHint') }}</span>
              </div>
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMemoryStore } from '@/stores/memory'
import type { GraphNode, GraphEdge, GraphData } from '@/api/memory/index'

const props = defineProps<{
  kbId: string
}>()

const { t } = useI18n()
const memoryStore = useMemoryStore()

// ---------------------------------------------------------------------------
// Reactive state
// ---------------------------------------------------------------------------
const svgContainerRef = ref<HTMLElement | null>(null)
const svgRef = ref<SVGSVGElement | null>(null)
const rootGRef = ref<SVGGElement | null>(null)

const depth = ref(2)
const legendCollapsed = ref(false)
const svgWidth = ref(800)
const svgHeight = ref(600)

// Interaction state
const draggingNodeId = ref<string | null>(null)
const hoveredNodeId = ref<string | null>(null)
let nodeDragMoved = false
let nodeDragThreshold = 5

// Force simulation state
interface SimNode extends GraphNode {
  x: number
  y: number
  vx: number
  vy: number
  pinned: boolean
}

interface SimEdge extends GraphEdge {
  sx: number
  sy: number
  tx: number
  ty: number
}

const displayNodes = ref<SimNode[]>([])
const displayEdges = ref<SimEdge[]>([])
const centerNodeId = ref<string | null>(null)

let animFrameId = 0
let simAlpha = 1.0
let allSimNodes: SimNode[] = []
let allSimEdges: SimEdge[] = []

// ---------------------------------------------------------------------------
// Node colors by type
// ---------------------------------------------------------------------------
const NODE_TYPE_COLORS: Record<string, string> = {
  episodic: '#1890ff',
  semantic: '#52c41a',
  procedural: '#fa8c16',
  decision: '#722ed1',
  preference: '#eb2f96',
  fact: '#8c8c8c',
}

const NODE_TYPE_LABELS: Record<string, string> = {
  episodic: 'episodic',
  semantic: 'semantic',
  procedural: 'procedural',
  decision: 'decision',
  preference: 'preference',
  fact: 'fact',
}

const typeLegendItems = computed(() =>
  Object.entries(NODE_TYPE_COLORS).map(([key, color]) => ({
    key,
    color,
    label: t(`memory.types.${NODE_TYPE_LABELS[key] || key}`),
  }))
)

const relationLegendItems = computed(() => [
  { key: 'related_to', color: '#8c8c8c', cssClass: 'line-solid', label: t('memory.graph.relatedTo') },
  { key: 'justifies', color: '#1890ff', cssClass: 'line-dashed', label: t('memory.graph.justifies') },
  { key: 'contradicts', color: '#e34d59', cssClass: 'line-dotted', label: t('memory.graph.contradicts') },
])

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------
function nodeColor(node: SimNode): string {
  return NODE_TYPE_COLORS[node.type] || '#bfbfbf'
}

function nodeBorder(node: SimNode): string {
  if (node.verdict === 'decision') return '#0052d9'
  return NODE_TYPE_COLORS[node.type] || '#bfbfbf'
}

function nodeRadius(node: SimNode): number {
  return Math.max(8, Math.min(20, 8 + Math.log(node.importance + 1) * 2))
}

function nodeLabelFontSize(node: SimNode): number {
  return nodeRadius(node) > 12 ? 11 : 10
}

function nodeOpacity(node: SimNode): number {
  if (node.verdict === 'refuted') return 0.4
  if (node.is_stale) return 0.55
  return 1
}

function labelOpacity(node: SimNode): number {
  if (node.verdict === 'refuted') return 0.5
  if (node.is_stale) return 0.65
  return 0.9
}

function edgeColor(relation: string): string {
  const map: Record<string, string> = {
    related_to: '#8c8c8c',
    justifies: '#1890ff',
    contradicts: '#e34d59',
  }
  return map[relation] || '#8c8c8c'
}

function edgeWeight(edge: SimEdge): number {
  return Math.max(1, Math.min(3, edge.weight * 2))
}

function edgeDash(relation: string): string {
  if (relation === 'justifies') return '5,4'
  if (relation === 'contradicts') return '3,3'
  return 'none'
}

const edgeOpacity = 0.5

// ---------------------------------------------------------------------------
// SVG pixel scaling (maintain consistent appearance across resize)
// ---------------------------------------------------------------------------
function npx(v: number): number {
  // Simple identity for now — SVG viewBox is our coordinate space
  return v
}

// ---------------------------------------------------------------------------
// Data loading
// ---------------------------------------------------------------------------
const graphData = computed(() => memoryStore.graphData)

watch(
  () => props.kbId,
  (id) => {
    if (id) {
      memoryStore.loadGraph(id)
    }
  },
  { immediate: true }
)

watch(graphData, (data) => {
  if (data?.nodes?.length) {
    buildSimulation(data)
    startSimulation()
  } else {
    stopSimulation()
    displayNodes.value = []
    displayEdges.value = []
  }
})

// ---------------------------------------------------------------------------
// Build simulation data from graph data
// ---------------------------------------------------------------------------
function buildSimulation(data: GraphData) {
  const width = svgContainerRef.value?.clientWidth || 800
  const height = svgContainerRef.value?.clientHeight || 600
  svgWidth.value = width
  svgHeight.value = height

  const nodeMap = new Map<string, SimNode>()

  // Build SimNodes, reusing existing positions if available
  const priorCoords = new Map<string, { x: number; y: number; vx: number; vy: number; pinned: boolean }>()
  for (const n of allSimNodes) {
    priorCoords.set(n.id, { x: n.x, y: n.y, vx: n.vx, vy: n.vy, pinned: n.pinned })
  }

  allSimNodes = data.nodes.map((n, i) => {
    const prior = priorCoords.get(n.id)
    let x: number, y: number, vx: number, vy: number, pinned: boolean
    if (prior) {
      x = prior.x
      y = prior.y
      vx = prior.vx
      vy = prior.vy
      pinned = prior.pinned
    } else {
      const angle = (2 * Math.PI * i) / data.nodes.length
      const r = Math.min(width, height) * 0.3
      x = width / 2 + r * Math.cos(angle) + (Math.random() - 0.5) * 30
      y = height / 2 + r * Math.sin(angle) + (Math.random() - 0.5) * 30
      vx = 0
      vy = 0
      pinned = false
    }
    const simNode: SimNode = {
      ...n,
      x, y, vx, vy, pinned,
    }
    nodeMap.set(n.id, simNode)
    return simNode
  })

  // Build SimEdges
  allSimEdges = data.edges.map((e) => {
    const s = nodeMap.get(e.source)
    const t = nodeMap.get(e.target)
    return {
      ...e,
      sx: s?.x ?? width / 2,
      sy: s?.y ?? height / 2,
      tx: t?.x ?? width / 2,
      ty: t?.y ?? height / 2,
    }
  })

  // Set initial center node
  if (!centerNodeId.value && allSimNodes.length > 0) {
    centerNodeId.value = allSimNodes[0].id
  }

  // Immediately filter by depth
  applyDepthFilter()
}

// ---------------------------------------------------------------------------
// Depth filtering (BFS from center node)
// ---------------------------------------------------------------------------
function applyDepthFilter() {
  if (!centerNodeId.value || allSimNodes.length === 0) {
    displayNodes.value = [...allSimNodes]
    displayEdges.value = [...allSimEdges]
    return
  }

  const centerId = centerNodeId.value

  // Build adjacency
  const adj = new Map<string, Set<string>>()
  for (const node of allSimNodes) {
    adj.set(node.id, new Set())
  }
  for (const edge of allSimEdges) {
    adj.get(edge.source)?.add(edge.target)
    adj.get(edge.target)?.add(edge.source)
  }

  // BFS
  const visited = new Set<string>()
  const queue: { id: string; dist: number }[] = [{ id: centerId, dist: 0 }]
  visited.add(centerId)

  while (queue.length > 0) {
    const { id, dist } = queue.shift()!
    if (dist >= depth.value) continue
    for (const neighbor of adj.get(id) || []) {
      if (!visited.has(neighbor)) {
        visited.add(neighbor)
        queue.push({ id: neighbor, dist: dist + 1 })
      }
    }
  }

  // Filter nodes
  const filteredNodeIds = visited
  displayNodes.value = allSimNodes.filter((n) => filteredNodeIds.has(n.id))

  // Filter edges (only edges where both endpoints are in the filtered set)
  const filteredNodeIdSet = new Set(filteredNodeIds)
  displayEdges.value = allSimEdges.filter(
    (e) => filteredNodeIdSet.has(e.source) && filteredNodeIdSet.has(e.target)
  )
}

// ---------------------------------------------------------------------------
// Force simulation
// ---------------------------------------------------------------------------
function startSimulation() {
  stopSimulation()
  simAlpha = 1.0
  animFrameId = requestAnimationFrame(tick)
}

function stopSimulation() {
  if (animFrameId) {
    cancelAnimationFrame(animFrameId)
    animFrameId = 0
  }
}

function tick() {
  if (!displayNodes.value.length) {
    animFrameId = 0
    return
  }

  simAlpha *= 0.99
  if (simAlpha < 0.02) {
    animFrameId = 0
    return
  }

  // Use allSimNodes for physics, but only displaySimNodes for rendering
  const nodes = allSimNodes
  const edges = allSimEdges
  const width = svgWidth.value
  const height = svgHeight.value
  const repulsionDistance = 250
  const repulsionDistanceSq = repulsionDistance * repulsionDistance

  // Repulsion (Coulomb's law) — O(n^2) with spatial culling
  const sorted = [...nodes].sort((a, b) => a.x - b.x)
  for (let i = 0; i < sorted.length; i++) {
    const n1 = sorted[i]
    for (let j = i + 1; j < sorted.length; j++) {
      const n2 = sorted[j]
      const dx = n2.x - n1.x
      if (dx > repulsionDistance) break // X-axis culling
      const dy = n2.y - n1.y
      if (Math.abs(dy) > repulsionDistance) continue // Y-axis culling
      const distSq = dx * dx + dy * dy
      if (distSq > repulsionDistanceSq) continue
      const dist = Math.sqrt(distSq) || 1
      const force = (400 * simAlpha) / Math.max(distSq, 50) * 50
      const fx = (dx / dist) * force
      const fy = (dy / dist) * force
      if (!n1.pinned) { n1.vx -= fx; n1.vy -= fy }
      if (!n2.pinned) { n2.vx += fx; n2.vy += fy }
    }
  }

  // Attraction along edges (spring force)
  for (const edge of edges) {
    const s = nodes.find((n) => n.id === edge.source)
    const t = nodes.find((n) => n.id === edge.target)
    if (!s || !t) continue
    const dx = t.x - s.x
    const dy = t.y - s.y
    const dist = Math.sqrt(dx * dx + dy * dy) || 1
    const targetDist = 100
    const force = (dist - targetDist) * 0.008 * simAlpha
    const fx = (dx / dist) * force
    const fy = (dy / dist) * force
    if (!s.pinned) { s.vx += fx; s.vy += fy }
    if (!t.pinned) { t.vx -= fx; t.vy -= fy }
  }

  // Center gravity
  const gravStrength = Math.min(0.008, 0.001 + nodes.length * 0.00001)
  for (const n of nodes) {
    if (n.pinned) continue
    n.vx += (width / 2 - n.x) * gravStrength * simAlpha
    n.vy += (height / 2 - n.y) * gravStrength * simAlpha
  }

  // Apply velocity
  for (const n of nodes) {
    if (n.pinned) continue
    n.vx *= 0.55
    n.vy *= 0.55
    const v = Math.sqrt(n.vx * n.vx + n.vy * n.vy)
    if (v > 15) {
      n.vx = (n.vx / v) * 15
      n.vy = (n.vy / v) * 15
    }
    n.x += n.vx
    n.y += n.vy
  }

  // Sync edge positions from current node positions
  const nodePosMap = new Map<string, { x: number; y: number }>()
  for (const n of nodes) {
    nodePosMap.set(n.id, { x: n.x, y: n.y })
  }

  // Update display edges positions
  for (const edge of displayEdges.value) {
    const sPos = nodePosMap.get(edge.source)
    const tPos = nodePosMap.get(edge.target)
    if (sPos && tPos) {
      edge.sx = sPos.x
      edge.sy = sPos.y
      edge.tx = tPos.x
      edge.ty = tPos.y
    }
  }

  // The displayNodes reference needs to be reactive — since allSimNodes
  // are mutated in place, Vue's reactivity won't track these changes.
  // We write back to the ref to trigger re-render, but only for visible nodes.
  // This is fine for our SVG binding since we use displayNodes in the template.
  displayNodes.value = [...displayNodes.value]

  animFrameId = requestAnimationFrame(tick)
}

// ---------------------------------------------------------------------------
// Pan / Zoom (simplified)
// ---------------------------------------------------------------------------
const panX = ref(0)
const panY = ref(0)
const zoomScale = ref(1)

function fitToScreen() {
  if (!displayNodes.value.length || !svgContainerRef.value) return

  const nodes = displayNodes.value
  const padding = 60
  const width = svgContainerRef.value.clientWidth
  const height = svgContainerRef.value.clientHeight

  let minX = Infinity, minY = Infinity, maxX = -Infinity, maxY = -Infinity
  for (const n of nodes) {
    if (n.x < minX) minX = n.x
    if (n.y < minY) minY = n.y
    if (n.x > maxX) maxX = n.x
    if (n.y > maxY) maxY = n.y
  }

  const graphW = maxX - minX || 1
  const graphH = maxY - minY || 1
  const scaleX = (width - padding * 2) / graphW
  const scaleY = (height - padding * 2) / graphH
  const s = Math.min(scaleX, scaleY, 2)

  zoomScale.value = s
  panX.value = (width - (minX + maxX) * s) / 2
  panY.value = (height - (minY + maxY) * s) / 2

  applyTransform()
}

function recenterGraph() {
  if (svgContainerRef.value) {
    panX.value = 0
    panY.value = 0
    zoomScale.value = 1
    applyTransform()
  }
}

function applyTransform() {
  const rootG = rootGRef.value
  if (!rootG) return
  rootG.setAttribute(
    'transform',
    `translate(${panX.value},${panY.value}) scale(${zoomScale.value})`
  )
}

// ---------------------------------------------------------------------------
// Mouse wheel zoom and pan
// ---------------------------------------------------------------------------
function onWheel(e: WheelEvent) {
  e.preventDefault()
  const delta = e.deltaY > 0 ? 0.92 : 1.08
  zoomScale.value = Math.max(0.2, Math.min(5, zoomScale.value * delta))
  applyTransform()
}

let bgDragging = false
let bgDragStartX = 0
let bgDragStartY = 0
let bgPanStartX = 0
let bgPanStartY = 0

function onBgMouseDown(e: MouseEvent) {
  if (e.button !== 0) return
  bgDragging = true
  bgDragStartX = e.clientX
  bgDragStartY = e.clientY
  bgPanStartX = panX.value
  bgPanStartY = panY.value
}

function onBgMouseMove(e: MouseEvent) {
  if (!bgDragging) return
  panX.value = bgPanStartX + (e.clientX - bgDragStartX)
  panY.value = bgPanStartY + (e.clientY - bgDragStartY)
  applyTransform()
}

function onBgMouseUp() {
  bgDragging = false
}

// ---------------------------------------------------------------------------
// Node drag
// ---------------------------------------------------------------------------
function onNodeMouseDown(e: MouseEvent, node: SimNode) {
  if (e.button !== 0) return
  e.stopPropagation()
  draggingNodeId.value = node.id
  node.pinned = true
  nodeDragMoved = false

  const rect = svgContainerRef.value?.getBoundingClientRect()
  if (!rect) return
  const startPx = (e.clientX - rect.left - panX.value) / zoomScale.value
  const startPy = (e.clientY - rect.top - panY.value) / zoomScale.value
  const nodeStartX = node.x
  const nodeStartY = node.y

  function onMove(ev: MouseEvent) {
    if (!rect) return
    const cx = (ev.clientX - rect.left - panX.value) / zoomScale.value
    const cy = (ev.clientY - rect.top - panY.value) / zoomScale.value
    const dx = cx - startPx
    const dy = cy - startPy
    if (Math.abs(dx) > nodeDragThreshold || Math.abs(dy) > nodeDragThreshold) {
      nodeDragMoved = true
    }
    node.x = nodeStartX + dx
    node.y = nodeStartY + dy
  }

  function onUp() {
    draggingNodeId.value = null
    window.removeEventListener('mousemove', onMove)
    window.removeEventListener('mouseup', onUp)
  }

  window.addEventListener('mousemove', onMove)
  window.addEventListener('mouseup', onUp)
}

// ---------------------------------------------------------------------------
// Node click (refocus center), skipped if user was dragging
// ---------------------------------------------------------------------------
function onNodeClick(node: SimNode) {
  if (nodeDragMoved) return
  setCenterNode(node.id)
}

// ---------------------------------------------------------------------------
// Pan/zoom via container events
// ---------------------------------------------------------------------------
onMounted(() => {
  const container = svgContainerRef.value
  if (!container) return

  container.addEventListener('wheel', onWheel, { passive: false })
  container.addEventListener('mousedown', onBgMouseDown)
  window.addEventListener('mousemove', onBgMouseMove)
  window.addEventListener('mouseup', onBgMouseUp)

  // Resize observer
  const ro = new ResizeObserver(() => {
    if (svgContainerRef.value) {
      svgWidth.value = svgContainerRef.value.clientWidth || 800
      svgHeight.value = svgContainerRef.value.clientHeight || 600
    }
  })
  ro.observe(container)

  // Also re-apply transform after mount
  applyTransform()
})

onUnmounted(() => {
  const container = svgContainerRef.value
  if (container) {
    container.removeEventListener('wheel', onWheel)
    container.removeEventListener('mousedown', onBgMouseDown)
  }
  window.removeEventListener('mousemove', onBgMouseMove)
  window.removeEventListener('mouseup', onBgMouseUp)

  stopSimulation()
})
</script>

<style scoped lang="less">
.memory-graph {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 8px;
}

// -----------------------------------------------------------------------
// Toolbar
// -----------------------------------------------------------------------
.graph-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  flex-shrink: 0;
  gap: 8px;
}

.toolbar-left {
  display: flex;
  align-items: center;
  gap: 8px;
}

.toolbar-label {
  font-size: 13px;
  color: var(--td-text-color-secondary);
  white-space: nowrap;
}

.toolbar-right {
  display: flex;
  align-items: center;
  gap: 4px;
}

// -----------------------------------------------------------------------
// Canvas
// -----------------------------------------------------------------------
.graph-canvas-wrapper {
  flex: 1;
  min-height: 0;
  position: relative;
  overflow: hidden;
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  background: var(--td-bg-color-secondarycontainer);
}

.graph-svg-container {
  width: 100%;
  height: 100%;
  cursor: grab;

  &:active {
    cursor: grabbing;
  }
}

.graph-svg {
  width: 100%;
  height: 100%;
  display: block;
}

.graph-bg {
  cursor: grab;
}

// -----------------------------------------------------------------------
// Edges
// -----------------------------------------------------------------------
.graph-edge {
  transition: opacity 0.2s;
}

.edge-related_to {
  stroke: #8c8c8c;
}

.edge-justifies {
  stroke: #1890ff;
  stroke-dasharray: 5, 4;
}

.edge-contradicts {
  stroke: #e34d59;
  stroke-dasharray: 3, 3;
}

// -----------------------------------------------------------------------
// Nodes
// -----------------------------------------------------------------------
.graph-node-circle {
  transition: stroke-width 0.15s, opacity 0.2s;
}

.graph-node-group {
  &.node-dragging {
    cursor: grabbing;
  }
}

.graph-node-label {
  fill: var(--td-text-color-primary);
  pointer-events: none;
  user-select: none;
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
}

// -----------------------------------------------------------------------
// Loading / Empty
// -----------------------------------------------------------------------
.loading-container {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 200px;
  height: 100%;
}

.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  min-height: 240px;
  height: 100%;
  padding: 40px;
}

.empty-icon {
  color: var(--td-text-color-disabled);
}

.empty-title {
  font-size: 16px;
  font-weight: 500;
  color: var(--td-text-color-primary);
}

.empty-desc {
  font-size: 13px;
  color: var(--td-text-color-placeholder);
  text-align: center;
  max-width: 400px;
  line-height: 1.5;
}

// -----------------------------------------------------------------------
// Legend
// -----------------------------------------------------------------------
.graph-legend {
  position: absolute;
  top: 12px;
  right: 12px;
  min-width: 160px;
  max-width: 220px;
  background: var(--td-bg-color-container);
  border: 1px solid var(--td-component-stroke);
  border-radius: 8px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.08);
  font-size: 12px;
  z-index: 10;

  &.collapsed {
    .legend-body {
      display: none;
    }
  }
}

.legend-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 8px 12px;
  cursor: pointer;
  font-weight: 500;
  font-size: 13px;
  color: var(--td-text-color-primary);
  user-select: none;

  &:hover {
    color: var(--td-brand-color);
  }
}

.legend-body {
  padding: 0 12px 10px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.legend-section {
  display: flex;
  flex-direction: column;
  gap: 3px;
}

.legend-section-title {
  font-size: 11px;
  font-weight: 600;
  color: var(--td-text-color-placeholder);
  margin-bottom: 2px;
}

.legend-item {
  display: flex;
  align-items: center;
  gap: 6px;
}

.legend-dot {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  flex-shrink: 0;
}

.legend-dot-dimmed {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #8c8c8c;
  opacity: 0.4;
  flex-shrink: 0;
}

.legend-dot-hub {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 2px dashed #ffa940;
  background: transparent;
  flex-shrink: 0;
}

.legend-ring-demo {
  display: inline-block;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  border: 2px solid #0052d9;
  background: transparent;
  flex-shrink: 0;
}

.legend-line {
  display: inline-block;
  width: 18px;
  height: 2px;
  flex-shrink: 0;
}

.line-solid {
  // default solid
}

.line-dashed {
  background: repeating-linear-gradient(
    to right,
    #1890ff 0,
    #1890ff 5px,
    transparent 5px,
    transparent 9px
  ) !important;
}

.line-dotted {
  background: repeating-linear-gradient(
    to right,
    #e34d59 0,
    #e34d59 3px,
    transparent 3px,
    transparent 6px
  ) !important;
}

.legend-label {
  font-size: 11px;
  color: var(--td-text-color-secondary);
}

.legend-hint {
  font-size: 10px;
  color: var(--td-text-color-placeholder);
  font-style: italic;
}
</style>
