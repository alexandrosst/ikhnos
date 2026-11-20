import {
  Background,
  BackgroundVariant,
  Controls,
  getNodesBounds,
  getViewportForBounds,
  Panel,
  ReactFlow,
  ReactFlowProvider,
  useNodesState,
  useReactFlow,
  useStoreApi,
  type Edge,
} from '@xyflow/react'
import '@xyflow/react/dist/style.css'
import { Filter as FilterIcon, Info, Plug, Plus, RotateCcw } from 'lucide-react'
import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useQueryParams } from '@/lib/useQueryParams'
import { useTopologyScope, useViewOptions, VIEWS } from '@/features/topology/useTopologyOptions'
import { useConnectFlow } from '@/features/discovery/components/ConnectFlow'
import { useTelemetryFlow } from '@/features/pipeline/components/telemetry/TelemetryFlow'
import { ClusterForm, DeviceForm, NodeForm, ServiceForm } from '@/components/forms'
import GettingStarted, { useGettingStarted } from '@/components/GettingStarted'
import Inspector, { type Selection } from '@/features/topology/components/Inspector'
import TopologyLegend from '@/features/topology/components/TopologyLegend'
// Lazily loaded, not a plain top-level import: MapView pulls in d3-geo, topojson-client and the
// placement-suggestion engine (usePlacementSuggestions) at its own module top level - real weight
// (~45KB gzipped) that a static import would put on every single visit to this page, Topology being
// the app's default landing route, even though most sessions render the Application or Infrastructure
// view and never open the Map tab at all. The raw map DATA (cities/countries topojson) was already its
// own further-lazy import inside MapView itself (see MapView.tsx's own loadCoarse/loadFine) - this just
// extends the same reasoning to MapView's own code, the one part of that story the module boundary
// here didn't previously cover.
const MapView = lazy(() => import('@/features/topology/components/MapView'))
import EdgeHoverCard, { type EdgeHoverPos } from '@/features/topology/components/EdgeHoverCard'
import LiveStatus from '@/features/topology/components/LiveStatus'
import { nodeTypes } from '@/features/topology/components/nodes'
import HopsControl from '@/features/topology/components/HopsControl'
import ViewOptionsMenu from '@/features/topology/components/ViewOptionsMenu'
import TelemetryTab from '@/features/topology/components/TelemetryTab'
import { edgeTypes } from '@/features/topology/components/OffsetEdge'
import { AutoMiniMap } from '@/features/topology/components/AutoMiniMap'
import { SquareRoutes } from '@/features/topology/components/SquareRoutes'
import { Button, EmptyState, ICON_SM, SkeletonBlock } from '@/components/ui/primitives'
import { Tabs } from '@/components/ui/Tabs'
import FilterMenu, { FilterChip } from '@/features/topology/components/FilterMenu'
import { computeFocus } from '@/features/topology/lib/focus'
import { narrowFitZoom } from '@/features/topology/lib/fit'
import { encodeList } from '@/features/topology/lib/filter'
import { applyGraphUpdate, buildGraph, cardId, groupId, sameLayout, syncSelected, type TopoEdge, type TopoNode } from '@/features/topology/lib/graph'
import { lossBand } from '@/lib/metrics'
import { lossLabel } from '@/features/topology/lib/lossLabel'
import { anyMesh, VERDICT_COLOR } from '@/features/topology/lib/mesh'
import { useAutoPlaceClusters } from '@/lib/usePlacement'
import { parseSel } from '@/lib/search'
import { TIER_COLOR } from '@/lib/types'
import { useServer } from '@/store/server'
import { useClusterLinks, usePaths, useTopology } from '@/store/topology'

/** Marker id for the open-chevron arrowhead in a given line colour. */
const arrowId = (color: string) => `topo-ah-${color.replace(/[^0-9a-z]/gi, '')}`
/** One open-chevron marker per line colour (an SVG marker cannot take the colour of the line it sits on in every browser). */
function ArrowDefs({ colors }: { colors: string[] }) {
  return (
    <svg width="0" height="0" style={{ position: 'absolute' }} aria-hidden>
      <defs>
        {colors.filter(Boolean).map((c) => (
          <marker key={c} id={arrowId(c)} viewBox="0 0 12 12" refX="8.5" refY="6" markerWidth="12" markerHeight="12" markerUnits="userSpaceOnUse" orient="auto-start-reverse">
            <path d="M1.5,1.5 L8.5,6 L1.5,10.5" fill="none" stroke={c} strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" />
          </marker>
        ))}
      </defs>
    </svg>
  )
}

/**
 * While a line is hovered it is drawn heavier and the two things it joins get an accent ring. Done with a tiny stylesheet rather than
 * node/edge state, so a mouse crossing the canvas re-renders nothing on the graph (see `edges` below).
 */
const cssStr = (v: string) => v.replace(/[\\"]/g, '\\$&')
function HoverStyle({ edge }: { edge: { id: string; source: string; target: string } | undefined }) {
  if (!edge) return null
  const ends = [edge.source, edge.target].map((id) => `.react-flow__node[data-id="${cssStr(id)}"] > div`).join(', ')
  return (
    <style>{`
      .react-flow__edge[data-id="${cssStr(edge.id)}"] .react-flow__edge-path { stroke-width: 3px !important; opacity: 1 !important; }
      ${ends} { box-shadow: inset 0 0 0 1.5px var(--color-accent) !important; }
    `}</style>
  )
}

type FormState =
  | { type: 'cluster'; id?: string }
  | { type: 'node'; id?: string }
  | { type: 'service'; id?: string }
  | { type: 'device'; id?: string }
  | null

/** The toolbar's popovers (filter, saved views, options, add) all hang off the same row: at most one may be
 * open at a time, so opening one always closes any other that was already open, instead of both fighting over
 * their own click-outside backdrop. */
type MenuKey = 'filter' | 'views' | 'options' | 'add' | 'more'

function Canvas() {
  const topology = useTopology()
  const { fitView, getNodes, setViewport, getNodesBounds: instanceBounds } = useReactFlow()
  const store = useStoreApi()
  // Fit the whole graph, but never below FIT_MIN_ZOOM: a graph bigger than the window then starts at its top
  // left (cropping only the far side, which scrolls into view) instead of cropping both ends equally.
  const fit = useCallback((duration: number) => {
    const { width, height } = store.getState()
    const bounds = getNodesBounds(getNodes())
    // Nothing measured yet: let React Flow wait for it.
    if (!width || !height || !bounds.width) { void fitView({ ...FIT_VIEW_OPTIONS, duration }); return }
    const v = getViewportForBounds(bounds, width, height, FIT_MIN_ZOOM, 1.75, FIT_PADDING)
    // On a phone the widest cluster box sets the zoom instead of the whole graph (see narrowFitZoom).
    const widest = Math.max(0, ...getNodes().filter((n) => n.type === 'boundary' && !n.parentId).map((n) => n.measured?.width ?? 0))
    const zoom = narrowFitZoom(width, v.zoom, widest)
    void setViewport({ x: Math.max(zoom === v.zoom ? v.x : -Infinity, width * 0.04 - bounds.x * zoom), y: Math.max(zoom === v.zoom ? v.y : -Infinity, height * 0.04 - bounds.y * zoom), zoom }, { duration })
  }, [store, fitView, getNodes, setViewport])
  const [sp, setParams] = useQueryParams()
  const connect = useConnectFlow()
  const telemetry = useTelemetryFlow()
  const started = useGettingStarted('topology')
  // The canvas is where a cluster's placement is actually seen, so it's a fair place to also resolve
  // a missing one silently (see Layout.tsx's comment for why this no longer runs on every route).
  useAutoPlaceClusters()

  const hasMesh = anyMesh(topology.clusters)
  // What the address says about how to draw (see useTopologyOptions): the options themselves, and the one-time default for a fresh Application view.
  const { setParam, ...options } = useViewOptions(sp, setParams, hasMesh)
  const { mode, isMap, isTelemetry, isCanvas, view, groupBy, servicesOnNodes, links, showDevices, showLabels, showNoise, showMesh, showNamespaces, showChain, showHealthLens, problemsOnly } = options

  const [selection, setSelection] = useState<Selection>(null)

  // Arriving from search (or a shared link) with ?sel=service:w-gw opens that thing in the inspector.
  const selParam = sp.get('sel')
  const [form, setForm] = useState<FormState>(null)
  const [openMenu, setOpenMenu] = useState<MenuKey | null>(null)
  // The legend is a small chip until someone wants it, so it does not sit on top of the graph; the choice is remembered.
  const [legendOpen, setLegendOpen] = useState(() => {
    try {
      return localStorage.getItem('ikhnos:topology-legend') === '1'
    } catch {
      return false
    }
  })
  const toggleLegend = (open: boolean) => {
    setLegendOpen(open)
    try {
      localStorage.setItem('ikhnos:topology-legend', open ? '1' : '0')
    } catch {
      /* private mode: the choice just isn't remembered */
    }
  }
  const toggleMenu = (key: MenuKey) => setOpenMenu((cur) => (cur === key ? null : key))
  const [hoverEdge, setHoverEdge] = useState<string | null>(null)
  // Cursor position for EdgeHoverCard (the throughput/latency popover a hovered edge shows - same anchoring
  // pattern as MapView's own LinkCard). Kept separate from `hoverEdge` itself: the id alone is enough to
  // drive the on-edge label reveal above, but the popover also needs to track the mouse to stay near it.
  const [hoverPos, setHoverPos] = useState<EdgeHoverPos | null>(null)
  // The canvas's own bounding box, for EdgeHoverCard to position itself against - set once the ReactFlow
  // wrapper mounts, same ref-callback pattern MapView uses for its own hover cards' `host`.
  const [host, setHost] = useState<HTMLElement | null>(null)
  // Escape closes whichever one of the toolbar's popovers is open.
  useEffect(() => {
    if (!openMenu) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpenMenu(null)
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [openMenu])

  const { clusters, nodes: machines, services, devices, dependencies, applications, sites, externalEndpoints, agents } = topology
  // Discovered records come from the server with the first refresh, after the workspace loads: a link to one waits for them.
  const observedReady = useServer((s) => s.status === 'disconnected' || s.state !== undefined)

  // Set when a selection arrives from search or a shared link: the view then goes to the entity (see the reveal effect below).
  const revealRef = useRef(false)
  // The entity the view just arrived at (from search): it glows for a moment so the eye finds it.
  const [landed, setLanded] = useState<string | null>(null)
  useEffect(() => {
    if (!selParam || !observedReady) return
    const want = parseSel(selParam)
    const pool: Record<string, { id: string }[]> = { cluster: clusters, node: machines, service: services, device: devices, site: sites, external: externalEndpoints }
    if (want && pool[want.kind].some((x) => x.id === want.id)) {
      setSelection(want)
      revealRef.current = true
    }
    setParams({ sel: null })
    // only the URL option triggers this; the lists are read when it arrives
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selParam, observedReady])
  const paths = usePaths()
  const clusterLinks = useClusterLinks()

  // What is shown: the filters, and a service's neighbourhood while one is selected (see useTopologyScope).
  const focusId = selection?.kind === 'service' ? selection.id : undefined
  const { filter, shown, hops, nothingMatches } = useTopologyScope(sp, topology, focusId)
  const graph = useMemo(
    () =>
      buildGraph(shown, {
        view, groupBy, servicesOnNodes, links, devices: showDevices, noise: showNoise, mesh: showMesh, namespaces: showNamespaces, chain: showChain, paths,
        clusterLinks,
      }),
    [shown, view, groupBy, servicesOnNodes, links, showDevices, showNoise, showMesh, showNamespaces, showChain, paths, clusterLinks],
  )

  const [nodes, setNodes, onNodesChange] = useNodesState<TopoNode>(graph.nodes)

  // React Flow's own multi-select (shift/ctrl/cmd-click, or a box-drag - see multiSelectionKeyCode below)
  // writes here via onSelectionChange, entirely separately from `selection` below (the single-click
  // Inspector state) - the two are merged into one `highlightedIds` set just below instead of one
  // overwriting the other, which is what used to make them "fight": a plain click used to reset every
  // node's `.selected` flag down to just the last-clicked id, silently erasing a multi-selection's own
  // highlight a moment after React Flow had just set it.
  const [multiSelectedIds, setMultiSelectedIds] = useState<string[]>([])

  // React Flow's SelectionListener effect (inside <ReactFlow>) is keyed on this callback's own identity,
  // not just on the selection actually changing - an inline arrow function passed straight in the JSX below
  // gets a fresh identity every render, so it fires again after every single render regardless of whether
  // anything was actually (re)selected. Combined with a plain `setMultiSelectedIds(sel.map(...))` - a new
  // array even when the ids are identical to what's already there - that turns into a genuine unconditional
  // render loop: render -> a "new" onSelectionChange -> React Flow calls it again -> setMultiSelectedIds
  // with a fresh-but-equal array -> React (correctly) re-renders because the reference changed -> repeat,
  // forever, with nothing in between ever actually settling. That is the real "Maximum update depth
  // exceeded" (React error #185) reported on the topology page - not the dragging race fixed elsewhere in
  // this file and in graph.ts, which only applies to filter/view-toggle timing, not this.
  //
  // Fixed the same way every other node/selection sync in this file already is (see syncSelected's and
  // applyGraphUpdate's own doc comments in graph.ts): a stable callback identity via useCallback, and a
  // bail-out to the exact same array reference when the ids didn't actually change, so setMultiSelectedIds
  // is a true no-op - and therefore triggers zero re-renders - once the selection has settled.
  const onSelectionChange = useCallback(({ nodes: sel }: { nodes: TopoNode[] }) => {
    setMultiSelectedIds((prev) => {
      const ids = sel.map((n) => n.id)
      if (ids.length === prev.length && ids.every((id, i) => id === prev[i])) return prev
      return ids
    })
  }, [])

  // React Flow id of the current single-click Inspector selection (if it is visible in this plane).
  const selectedRfId = useMemo(() => {
    if (!selection) return null
    if (selection.kind === 'cluster') return groupBy === 'cluster' ? groupId(selection.id) : null
    if (selection.kind === 'tier') return groupId(selection.id)
    if (selection.kind === 'site') return groupId(`dev:${selection.id}`)
    return cardId(selection.id)
  }, [selection, groupBy])

  // The full set of node ids that should currently show the accent halo and count toward a telemetry scope:
  // the single-click Inspector selection and the multi-select set above, merged - so a scope built up across
  // several clicks (or a single click, or a box-drag over a whole cluster) all read as one consistent,
  // highlighted group instead of only the last-clicked item mattering.
  const highlightedIds = useMemo(() => {
    const s = new Set(multiSelectedIds)
    if (selectedRfId) s.add(selectedRfId)
    return s
  }, [selectedRfId, multiSelectedIds])


  // Re-sync when the model / plane changes (keeps selection highlight). On an ordinary background poll, a
  // node that was already on the canvas keeps the position it has there (a manual drag, or a prior layout
  // pass) instead of jumping back to the graph's freshly computed one, which would otherwise happen every
  // 2-5s even for a poll that changed nothing about this node - see resyncNodes (graph.ts) for the full
  // rule, including why a node React Flow is actively dragging is left completely untouched.
  //
  // An EXPLICIT interaction - a filter, or any other view toggle - gets a full fresh layout instead: see
  // applyGraphUpdate's own doc comment (graph.ts) for why the poll-time merge above leaves stale, messy
  // positions for a filter specifically (survivors keep the same parent, so the merge's own "parent changed"
  // escape hatch never fires for them, even though buildGraph repacked the whole group around them).
  // Every filter and view toggle on this page goes through the URL's search params, and a poll never
  // touches them, so "did `sp` itself change since last render" is exactly that distinction.
  const prevSpRef = useRef(sp)
  const prevGraphRef = useRef(graph)
  useEffect(() => {
    // A toggle that leaves every box where it was (DNS & system traffic only adds or removes lines) is not a relayout: what is on the canvas stays.
    const explicit = prevSpRef.current !== sp && !sameLayout(prevGraphRef.current.nodes, graph.nodes)
    prevSpRef.current = sp
    prevGraphRef.current = graph
    setNodes((prev) => applyGraphUpdate(prev, graph.nodes, highlightedIds, explicit))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graph, setNodes])
  // See syncSelected's own doc comment (graph.ts) for why this needs to both skip a node mid-drag and bail
  // out to the exact same array when nothing changed - together, the fix for a real "Maximum update depth
  // exceeded" crash (React error #185) that a click or drag between two nodes could trigger.
  //
  // Keyed on `selectedRfId` alone, deliberately NOT on `multiSelectedIds` (or `highlightedIds` itself, which
  // mixes both). `selectedRfId` is the single-click Inspector selection, which can change from OUTSIDE the
  // canvas entirely (e.g. clicking a related entity inside the Inspector panel) - React Flow has no way to
  // know about that on its own, so re-applying it here is genuinely necessary. `multiSelectedIds`, on the
  // other hand, is populated FROM React Flow's own selection via onSelectionChange above: by the time a
  // multi-select change reaches this component, React Flow's own nodes already carry the right `.selected`
  // flags for it - that's the very state onSelectionChange just read. Re-running this effect (and therefore
  // re-writing `.selected` on every node) merely because `multiSelectedIds` changed was writing that same
  // information back a second time through a different effect - which React Flow then dutifully reports
  // again via onSelectionChange, changing multiSelectedIds again, re-firing this effect again, forever. A
  // live headless-browser repro caught this as a real, sustained content-level oscillation (not just a
  // referential-identity one, which onSelectionChange's own fix above already closes) between a node just
  // clicked, that plus a leftover multi-selected node, and nothing at all - under rapid view-toggling
  // interleaved with clicks, and it kept looping on its own with no further input needed once started. Only
  // `selectedRfId` actually needs an imposed write here; `highlightedIds` (read fresh from the closure
  // below, not listed as a dependency) still correctly includes both pieces for that write itself - only the
  // trigger is narrowed, closing the loop.
  useEffect(() => {
    setNodes((ns) => syncSelected(ns, highlightedIds))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedRfId, setNodes])

  // Re-fit the viewport whenever the *shape* of the graph changes (not on every edit).
  const shape = useMemo(() => graph.nodes.map((n) => `${n.id}:${n.style?.width}x${n.style?.height}`).join('|'), [graph])
  useEffect(() => {
    // Layout's ErrorBoundary remounts this whole page on every navigation (`key={pathname}` - see its own
    // comment for why that's load-bearing for error recovery, not something to remove here), so this effect
    // - and the animated pan/zoom below - reruns on every single visit to this tab, not just a cold one.
    // Animating that settle is worth it the first time a session sees the canvas (there's no prior viewport
    // to jump from), but repeating a 300ms pan on every revisit is exactly the kind of "not instant" tab
    // switch this is meant to avoid. hasEverFit is module state, not component state, on purpose: it must
    // survive this component's own remount, and only a full page reload should reset it.
    const animate = !hasEverFit
    hasEverFit = true
    const t = setTimeout(() => fit(animate ? 300 : 0), animate ? 60 : 0)
    return () => clearTimeout(t)
  }, [shape, fit])

  // Edge styling for the current selection (color, width, dashing, opacity, and whether the label is shown
  // for a reason other than hover - selection/showLabels). Deliberately NOT keyed on `hoverEdge`: see `edges`
  // below for why.
  // The entity a click focused, by the id the edges refer to it with.
  const focusEntity =
    selection?.kind === 'service' || selection?.kind === 'device' || selection?.kind === 'external'
      ? selection.id
      : selection?.kind === 'cluster' && groupBy === 'cluster'
        ? selection.id
        : selection?.kind === 'tier'
          ? selection.id
          : null
  const pickedEdgeId = selection?.kind === 'dependency' ? selection.id : null

  // Focus: what stays lit while everything else fades - the focused entity, the entities its lines reach, and the boxes they
  // sit in (and, for a lit cluster or group box, what is inside it). `ripple` is how long each node waits before it fades,
  // growing with its distance from the focus, so the fade spreads outward from what was clicked.
  const focusView = useMemo(() => computeFocus(nodes, graph.edges, focusEntity ? selectedRfId : null, pickedEdgeId), [graph.edges, nodes, focusEntity, pickedEdgeId, selectedRfId])

  // The canvas nodes with the fade applied (a class and a delay; the transition is in index.css).
  const displayNodes = useMemo<TopoNode[]>(() => {
    if (!focusView && !landed) return nodes
    return nodes.map((n) => {
      if (n.id === landed) return { ...n, className: n.className ? `${n.className} topo-land` : 'topo-land' }
      if (!focusView || focusView.keep.has(n.id)) return n
      return { ...n, className: n.className ? `${n.className} topo-dim` : 'topo-dim', style: { ...n.style, '--ripple': `${focusView.ripple.get(n.id) ?? 0}ms` } as React.CSSProperties }
    })
  }, [nodes, focusView, landed])

  // The canvas can be panned only so far past the graph (and zoomed only between 0.2x and 1.5x), so the structure is never
  // lost off screen or shrunk into a smudge.
  const graphBounds = useMemo(() => {
    let minX = Infinity
    let minY = Infinity
    let maxX = -Infinity
    let maxY = -Infinity
    const byId = new Map(nodes.map((n) => [n.id, n]))
    for (const n of nodes) {
      let x = 0
      let y = 0
      for (let m: TopoNode | undefined = n; m; m = m.parentId ? byId.get(m.parentId) : undefined) {
        x += m.position.x
        y += m.position.y
      }
      minX = Math.min(minX, x)
      minY = Math.min(minY, y)
      maxX = Math.max(maxX, x + (n.measured?.width ?? n.width ?? 0))
      maxY = Math.max(maxY, y + (n.measured?.height ?? n.height ?? 0))
    }
    return Number.isFinite(minX) ? { minX, minY, maxX, maxY } : null
  }, [nodes])
  const panExtent = useMemo<[[number, number], [number, number]]>(() => {
    if (!graphBounds) return [[-Infinity, -Infinity], [Infinity, Infinity]]
    const pad = 600
    return [[graphBounds.minX - pad, graphBounds.minY - pad], [graphBounds.maxX + pad, graphBounds.maxY + pad]]
  }, [graphBounds])

  // Focusing an entity brings what it connects to into view: when the entity and the things its lines reach do not all fit
  // on screen (the Inspector takes a third of the width as it opens), the view pans, and zooms out only if it must.
  const focusCoreKey = focusView ? [...focusView.core].sort().join('|') : ''
  useEffect(() => {
    if (!focusView || !focusCoreKey) return
    const core = [...focusView.core]
    const t = setTimeout(() => {
      const { width, height, transform } = store.getState()
      if (!width || !height) return
      const box = instanceBounds(core)
      if (!box.width || !box.height) return
      const [tx, ty, zoom] = transform
      const margin = 32
      const inView = box.x * zoom + tx >= margin && box.y * zoom + ty >= margin && (box.x + box.width) * zoom + tx <= width - margin && (box.y + box.height) * zoom + ty <= height - margin
      if (inView) return
      const v = getViewportForBounds(box, width, height, 0.2, zoom, 0.16)
      const calm = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
      void setViewport(v, { duration: calm ? 0 : 380 })
    }, 220)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- keyed on which entities are in focus, not on every node move
  }, [focusCoreKey])

  // Arriving from search: go to the entity, not just open the Inspector. The card is centred at a readable zoom (with what its
  // lines reach, when it has any), and its pop plays as it lands.
  useEffect(() => {
    if (!revealRef.current || !selectedRfId) return
    const id = selectedRfId
    const t = setTimeout(() => {
      if (!revealRef.current) return
      const { width, height, transform } = store.getState()
      if (!width || !height) return
      const box = instanceBounds(focusView ? [...focusView.core] : [id])
      if (!box.width || !box.height) return
      revealRef.current = false
      const v = getViewportForBounds(box, width, height, 0.2, Math.max(transform[2], 0.9), 0.3)
      const calm = typeof window.matchMedia === 'function' && window.matchMedia('(prefers-reduced-motion: reduce)').matches
      void setViewport(v, { duration: calm ? 0 : 520 })
      window.setTimeout(() => setLanded(id), calm ? 0 : 480)
      window.setTimeout(() => setLanded((cur) => (cur === id ? null : cur)), calm ? 0 : 480 + 1500)
    }, 450)
    return () => clearTimeout(t)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- runs when a new entity is selected; the rest is read when it fires
  }, [selectedRfId])

  // Edge styling for the current selection (colour, width, dashing, opacity, and whether the label shows for a reason other
  // than hover). Deliberately NOT keyed on `hoverEdge`: see `edges` below for why.
  const styledEdges = useMemo<TopoEdge[]>(() => {
    return graph.edges.map((e) => {
      const hot = !!focusView?.hotIds.has(e.id)
      // A line fades unless it is one of the focus's own, or joins two things that both stay lit.
      const dim = !!focusView && !hot && !(focusView.keep.has(e.source) && focusView.keep.has(e.target))
      const showLabel = showLabels || hot
      const q = e.data?.quality
      const band = q ? lossBand(q.lossPct) : 'ok'
      // A link that loses connection attempts is coloured by how badly; otherwise grey, or orange when it is the focus.
      const mv = e.data?.mesh
      const cl = e.data?.clusterLink
      // A measured cluster link is never a dependency (no quality/mesh verdict can coexist with it), so there
      // is no precedence to resolve, only `hot` (selection focus) still wins. Plain cross-cluster grey unless the
      // health lens is on, which colours it by its measured loss.
      const clHealthBand = showHealthLens && cl?.avgLossPct !== undefined ? lossBand(cl.avgLossPct) : null
      const clHealthColor = clHealthBand === 'hot' ? '#f87171' : clHealthBand === 'warn' ? '#fbbf24' : clHealthBand === 'ok' ? '#34d399' : null
      const stroke = hot ? '#f68330' : clHealthColor ?? (mv ? VERDICT_COLOR[mv.state] : band === 'hot' ? '#f87171' : band === 'warn' ? '#fbbf24' : e.data?.crossGroup ? '#98a4ae' : '#6f7b85')
      // Seen in traffic: solid, and a touch thicker the busier it is. Only declared (or gone quiet): dotted and
      // thin. Kept close to the declared baseline (1.2) rather than scaling up hard - a busy link should read as
      // "more traffic" without out-weighing the 2.4px used for the current selection/focus.
      const seen = !!e.data?.observed && !e.data?.stale
      const width = hot ? 2.4 : cl ? 1.8 : e.data?.aggregated ? 2 : seen ? 1.2 + 1.0 * (e.data?.weight ?? 0.15) : 1.2
      // A seen edge with no `via` at all can't happen (isObserved only ever sets true alongside via), so
      // this only ever fires for a real conntrack-only edge - one whose traffic numbers, if it shows any,
      // are connection counts only (see EdgeData.via's own comment): a long, open dash reads as "mostly
      // solid but not fully confirmed" without competing with the short, tight '2 5' used for "not seen".
      const conntrackOnly = seen && e.data?.via === 'conntrack'
      return {
        ...e,
        className: hot ? [e.className, 'topo-hot'].filter(Boolean).join(' ') : e.className,
        // A line that loses connection attempts says so on itself, in its own colour, so it can be found without hovering.
        label: showLabel ? e.label : !dim ? lossLabel(q?.lossPct, band) : undefined,
        style: {
          stroke,
          strokeWidth: width,
          opacity: dim ? 0.15 : e.data?.stale ? 0.55 : 1,
          // The fade spreads out from the focus: a line goes when the nearer of the two things it joins does.
          transitionDelay: dim && focusView ? `${Math.min(focusView.ripple.get(e.source) ?? 0, focusView.ripple.get(e.target) ?? 0)}ms` : undefined,
          // A measured cluster link is always solid: whether anything was observed is not what it says.
          strokeDasharray: cl ? undefined : !e.data?.aggregated && !seen ? '2 5' : conntrackOnly ? '8 4' : undefined,
        },
        labelStyle: { fill: hot ? '#f68330' : !showLabel && band !== 'ok' ? stroke : '#a7b1b9', fontSize: 10.5, opacity: dim ? 0.3 : 1 },
        labelBgStyle: { fill: 'var(--color-nb-910)', fillOpacity: 0.95, stroke: !showLabel && !hot && band !== 'ok' ? stroke : undefined, strokeOpacity: 0.6 },
        labelBgPadding: [6, 3] as [number, number],
        labelBgBorderRadius: 4,
        markerEnd: e.markerEnd ? arrowId(stroke) : undefined,
      }
    })
  }, [graph.edges, focusView, showLabels, showHealthLens])

  // The raw (un-hidden) label text for every edge, so the hover-only reveal below can put one back without
  // needing to keep the whole graph.edges array around.
  const rawLabelById = useMemo(() => new Map(graph.edges.map((e) => [e.id, e.label])), [graph.edges])

  // Hovering only ever reveals ONE edge's label. Re-deriving the whole styled array (recomputing color/width/
  // dash/opacity for every edge, and - critically - handing React Flow a brand new object for every edge) on
  // every hover transition would force every OffsetEdge to re-render just because a mouse crossed the canvas
  // (it calls useInternalNode twice per edge - the most common canvas interaction there is, made needlessly
  // heavy). So this only ever swaps in a fresh object for the one edge whose shown-ness actually changed;
  // every other edge keeps the exact same object reference it already had.
  const edges = useMemo<TopoEdge[]>(() => {
    if (!hoverEdge) return styledEdges
    return styledEdges.map((e) => (e.id === hoverEdge && e.label !== rawLabelById.get(e.id) ? { ...e, label: rawLabelById.get(e.id) } : e))
  }, [styledEdges, hoverEdge, rawLabelById])

  // React-flow node id -> its own display title, for EdgeHoverCard: an edge's `source`/`target` ARE already
  // that id (cardId/groupId - see makeEdge in graph.ts), whether it's a service/machine/device card or a
  // cluster/tier/operator group box, so this one map names either end of any edge without needing to know
  // which kind of thing it points at.
  const nodeTitleById = useMemo(() => {
    const m = new Map<string, string>()
    for (const n of nodes) m.set(n.id, 'title' in n.data ? n.data.title : n.id)
    return m
  }, [nodes])
  const hoveredEdge = hoverEdge ? edges.find((e) => e.id === hoverEdge) : undefined

  const select = useCallback((s: Selection) => setSelection(s), [])

  const fromNode = (n: TopoNode): Selection => {
    const d = n.data
    if (d.kind === 'group') {
      if (d.extra === 'devices') return { kind: 'site', id: d.entityId }
      if (d.extra) return null
      return { kind: d.groupBy === 'cluster' ? 'cluster' : 'tier', id: d.entityId }
    }
    if (d.kind === 'namespace') return null // a visual grouping only, nothing to inspect on its own
    return { kind: d.kind === 'machine' ? 'node' : d.kind, id: d.entityId }
  }

  const editSelection = (s: NonNullable<Selection>) => {
    if (s.kind === 'cluster') setForm({ type: 'cluster', id: s.id })
    else if (s.kind === 'node') setForm({ type: 'node', id: s.id })
    else if (s.kind === 'service') setForm({ type: 'service', id: s.id })
    else if (s.kind === 'device') setForm({ type: 'device', id: s.id })
  }

  const miniColor = (n: { data?: unknown }) => {
    const d = n.data as TopoNode['data'] | undefined
    return d ? TIER_COLOR[d.tier] : '#3f444b'
  }

  const empty = clusters.length === 0
  const liveClusters = useMemo(() => clusters.filter((c) => !c.deletedAt), [clusters])
  const closeForm = () => setForm(null)

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/*
        Deliberately not PageHeader here, unlike every other page: PageHeader's spacious title + description
        + simple action-button row is built for scrolling content, and its `mb-6` alone would eat a chunk of
        the vertical space this page instead spends on the canvas filling the rest of the viewport. This
        toolbar's own content - a view-mode tablist, several dropdown menus (filter/views/options/add), and
        inline toggles - also doesn't fit PageHeader's title/description/actions shape to begin with. A
        compact, dense single-row bar is the right tool for a canvas page; PageHeader stays for pages that
        are actually a page of content.
      */}
      {/* Toolbar */}
      <div className="flex min-h-14 shrink-0 flex-wrap items-center gap-x-4 gap-y-2 border-b border-nb-850 bg-nb-920 px-4 py-2 sm:px-5">
        <h1 className="text-base font-medium text-nb-300">Topology</h1>
        <Tabs
          variant="segmented"
          label="View"
          value={mode}
          items={VIEWS.map((v) => ({ id: v.value, label: v.label, title: v.hint }))}
          onChange={(v) => {
            setParam('view', v === 'application' ? null : v)
            setSelection(null)
          }}
        />

        <div className="ml-auto flex flex-wrap items-center gap-2 sm:gap-3">
          <LiveStatus />
          {mode === 'application' && selection?.kind === 'service' && <HopsControl hops={hops} onChange={(n) => setParam('hops', n === undefined ? null : String(n))} />}
          {isTelemetry ? null : (
            <>
          <FilterChip filter={filter} onClear={() => { setParams({ clusters: null, apps: null, kinds: null }); setSelection(null) }} />
          <FilterMenu
            disabled={empty}
            open={openMenu === 'filter'}
            onOpenChange={(o) => setOpenMenu(o ? 'filter' : null)}
            filter={filter}
            clusters={clusters.filter((c) => !c.deletedAt)}
            applications={applications.filter((a) => !a.deletedAt)}
            onChange={(f) => {
              setParams({ clusters: encodeList(f.clusters), apps: encodeList(f.apps), kinds: encodeList(f.kinds) })
              setSelection(null)
            }}
          />
            </>
          )}
          {isCanvas && (
            <ViewOptionsMenu
              options={options}
              hasMesh={hasMesh}
              hasNoise={dependencies.some((d) => d.noise)}
              open={openMenu === 'options'}
              onToggle={() => toggleMenu('options')}
              onClose={() => setOpenMenu(null)}
              disabled={empty}
              setParam={setParam}
              // Hiding devices removes the selected one from the canvas; Inspector would otherwise keep showing (and let you edit) something no longer drawn anywhere.
              onDevicesHidden={() => { if (selection?.kind === 'device' || selection?.kind === 'external') setSelection(null) }}
              onGroupChanged={() => setSelection(null)}
            />
          )}

          {isCanvas && (
            <Button
              disabled={empty}
              onClick={() => {
                setNodes(graph.nodes)
                // The `shape` effect above only re-fits when node ids/sizes change, which a layout reset never does
                // (same nodes, new positions): without this, a reset whose new positions land outside the current
                // viewport looked like the button did nothing at all.
                fit(200)
              }}
              aria-label="Reset layout"
              title="Snap every entity back to its computed position. Your view options (filters, grouping, toggles) are untouched."
              data-testid="reset-layout"
            >
              <RotateCcw size={ICON_SM} />
            </Button>
          )}
        </div>
      </div>

      {isTelemetry && !empty ? (
        <TelemetryTab
          clusters={liveClusters}
          agents={agents}
          problemsOnly={problemsOnly}
          selection={selection}
          onSelect={select}
          onProblemsOnly={(on) => { setParam('problems', on ? '1' : null); setSelection(null) }}
          onConnect={connect.start}
          onClose={() => setSelection(null)}
        />
      ) : (
      <div className="flex min-h-0 flex-1">
        <div className="relative min-w-0 flex-1" ref={setHost}>
          {!empty && nothingMatches ? (
            <div className="grid h-full place-items-center p-8">
              <EmptyState
                title="Nothing matches this filter"
                description="None of the chosen clusters run the chosen applications. Widen the filter, or clear it to see everything again."
                action={<Button onClick={() => setParams({ clusters: null, apps: null })} data-testid="clear-filter"><FilterIcon size={ICON_SM} /> Clear the filter</Button>}
              />
            </div>
          ) : empty ? (
            <div className="grid h-full place-items-center overflow-y-auto p-6 sm:p-8">
              {started && !started.dismissed ? (
                <div className="flex w-full flex-col items-center gap-5 py-4" data-testid="empty-topology">
                  <div className="text-center">
                    <h2 className="text-base font-medium text-nb-300">Your topology is empty</h2>
                    <p className="mx-auto mt-1 max-w-md text-sm text-nb-500">Nothing is connected yet. These steps take a cluster from nothing to live; ikhnos then discovers its nodes, services and traffic itself.</p>
                  </div>
                  <GettingStarted variant="hero" checklist={started.checklist} onConnect={connect.start} />
                  <div className="flex flex-wrap items-center justify-center gap-2 text-sm text-nb-500">
                    or describe a cluster yourself
                    <Button size="sm" onClick={() => setForm({ type: 'cluster' })}><Plus size={ICON_SM} /> Add manually</Button>
                  </div>
                </div>
              ) : (
                <EmptyState
                  title="Your topology is empty"
                  description="Connect a cluster and let ikhnos discover its nodes, services and traffic, or describe one by hand. You can also load the sample under Settings → Import / Export."
                  action={
                    <div className="flex flex-wrap justify-center gap-2">
                      <Button variant="primary" onClick={connect.start} data-testid="empty-connect">
                        <Plug size={ICON_SM} /> Connect a cluster
                      </Button>
                      <Button onClick={() => setForm({ type: 'cluster' })}><Plus size={ICON_SM} /> Add manually</Button>
                    </div>
                  }
                />
              )}
            </div>
          ) : isMap ? (
            <Suspense fallback={<div className="h-full w-full p-4"><SkeletonBlock className="h-full w-full" /></div>}>
              <MapView selection={selection} onSelect={select} filter={filter} />
            </Suspense>
          ) : (
            <>
            <ArrowDefs colors={[...new Set(edges.map((e) => String(e.style?.stroke ?? '')))]} />
            <SquareRoutes edges={edges}>
            <ReactFlow<TopoNode, Edge>
              nodes={displayNodes}
              edges={edges}
              nodeTypes={nodeTypes}
              edgeTypes={edgeTypes}
              onNodesChange={onNodesChange}
              // A dragged box carries its contents: they must move with it at once, not ease after it (index.css).
              onNodeDragStart={() => host?.classList.add('topo-dragging')}
              onNodeDragStop={() => host?.classList.remove('topo-dragging')}
              onSelectionChange={onSelectionChange}
              onNodeClick={(e, n) => {
                // Same technique, for the per-pod expand panel's own node-name buttons (nodes.tsx's Card):
                // those live inside a SERVICE card but need to select a DIFFERENT entity (the node that
                // pod happens to be scheduled on) - something only this handler, not the card component
                // itself, has the selection setters to do. data-select-node carries that node's id.
                const nodeBtn = (e.target as HTMLElement).closest?.('[data-select-node]')
                const selectNodeId = nodeBtn?.getAttribute('data-select-node')
                if (selectNodeId) {
                  setMultiSelectedIds((prev) => (prev.length === 0 ? prev : []))
                  select({ kind: 'node', id: selectNodeId })
                  // syncSelected checks React Flow node ids, which are cardId(...)-prefixed - not the
                  // raw MachineNode id data-select-node carries (see cardId's call sites in graph.ts).
                  setNodes((ns) => syncSelected(ns, new Set([cardId(selectNodeId)])))
                  return
                }
                // A multi-select click (shift/ctrl/cmd, matching multiSelectionKeyCode below) has already
                // been folded into React Flow's own selection at the library level by the time this fires,
                // which onSelectionChange picks up into multiSelectedIds - leave it at that. Driving the
                // single-click Inspector `selection` for it too would change selectedRfId and, through it,
                // highlightedIds, but only ever to a single id - which would fight the very multi-selection
                // this click just added to a moment later.
                if (e.shiftKey || e.metaKey || e.ctrlKey) return
                // A plain click replaces whatever was selected with just this one node - that's the whole
                // point of a plain click vs. a shift/ctrl one. But a *prior* shift/ctrl-click or box-drag
                // leaves multiSelectedIds populated, and nothing about changing selectedRfId below clears
                // it on its own: the effect that paints `.selected` onto the canvas is keyed on
                // selectedRfId alone (see its own doc comment for why), so a plain re-click of a node that
                // was ALREADY the single-click selection before the multi-select even started changes
                // nothing about selectedRfId's own VALUE - same string, so React correctly skips that
                // effect - and React Flow's own native click handling also does nothing in exactly this
                // case (it only adds-or-removes a node from the selection; a node that's already selected
                // with no multi-select key held gets neither). Nothing was left to paint `.selected`
                // correctly, a real, reproducible bug (a plain click on one card, then another - or even
                // the same one again - left every previously multi-selected card still highlighted).
                // Fixed directly here instead of widening the effect's own dependency array (which is
                // deliberately narrow - see its comment on the render loop that made it that way): clear
                // the stale multi-selection and paint exactly this one node's `.selected` eagerly, right in
                // the click handler itself, so the result never depends on whether some other effect
                // happens to re-run afterward. syncSelected itself still no-ops (same array back) when the
                // canvas already matches, so this costs nothing on the common case where nothing was stale.
                setMultiSelectedIds((prev) => (prev.length === 0 ? prev : []))
                select(fromNode(n))
                setNodes((ns) => syncSelected(ns, new Set([n.id])))
              }}
              onPaneClick={() => {
                // Closes the Inspector (the single-click `selection`) - but a prior shift/ctrl-click or
                // box-drag multi-selection is tracked entirely separately (`multiSelectedIds`, populated
                // from React Flow's own selection via onSelectionChange - see its doc comment) and
                // `select(null)` alone never touched it, so clicking empty canvas after multi-selecting a
                // few cards left them all still highlighted, the floating scope toolbar still open, and
                // React Flow's own `.selected` flags still set - a real, reproducible gap (confirmed via a
                // live-browser check, not just reasoning about the code), not just a hypothetical one.
                // Cleared directly here, on the node array itself, rather than only through
                // setMultiSelectedIds: the effect that would otherwise re-sync `.selected` from
                // highlightedIds is deliberately keyed on `selectedRfId` alone (see its own doc comment, on
                // why watching multiSelectedIds too would loop), so a multi-select-only clear (no Inspector
                // selection open at all) would never reach it.
                select(null)
                setMultiSelectedIds((prev) => (prev.length === 0 ? prev : []))
                setNodes((ns) => syncSelected(ns, new Set()))
              }}
              onEdgeClick={(_, e) => { if (!e.data?.aggregated) select({ kind: 'dependency', id: e.id }) }}
              onEdgeMouseEnter={(e, edge) => { setHoverEdge(edge.id); setHoverPos({ cx: e.clientX, cy: e.clientY }) }}
              onEdgeMouseMove={(e) => setHoverPos({ cx: e.clientX, cy: e.clientY })}
              onEdgeMouseLeave={() => { setHoverEdge(null); setHoverPos(null) }}
              nodesConnectable={false}
              multiSelectionKeyCode={['Shift', 'Meta', 'Control']}
              minZoom={0.2}
              maxZoom={1.5}
              translateExtent={panExtent}
              fitView
              fitViewOptions={FIT_VIEW_OPTIONS}
              proOptions={PRO_OPTIONS}
              colorMode="dark"
              className="topo-canvas"
            >
              <Background variant={BackgroundVariant.Dots} gap={22} size={1.2} color="var(--color-nb-850)" />
              <Controls showInteractive={false} />
              {/* Hidden while the Inspector is open: the canvas is narrower then and the map would sit on the legend. The mask is themed in index.css. */}
              <AutoMiniMap bounds={graphBounds} hidden={!!selection} className="!hidden sm:!block" pannable zoomable nodeColor={miniColor} nodeStrokeWidth={0} />
              <Panel position="bottom-left" className="!mb-3 !ml-16 hidden sm:block">
                {legendOpen ? (
                  <TopologyLegend
                    tiers={[...new Set(shown.clusters.map((c) => c.tier))]}
                    groupBy={groupBy}
                    mesh={mode === 'application' && showMesh}
                    traffic={mode === 'application' && dependencies.length > 0}
                    connectionsOnly={mode === 'application' && dependencies.length > 0 && graph.edges.some((e) => e.data?.via === 'conntrack' && e.data?.observed && !e.data?.stale)}
                    clusterLinks={graph.edges.some((e) => e.data?.clusterLink)}
                    healthLens={showHealthLens}
                    pods={graph.nodes.some((n) => n.type === 'card' && n.data.pods)}
                    onClose={() => toggleLegend(false)}
                  />
                ) : (
                  <button
                    type="button"
                    onClick={() => toggleLegend(true)}
                    className="topo-glass flex items-center gap-1.5 rounded-xl border border-nb-850 px-2.5 py-1.5 text-xs text-nb-400 hover:text-nb-200"
                    aria-label="Show legend"
                    title="What the colours and line styles mean"
                    data-testid="legend-toggle"
                  >
                    <Info size={ICON_SM} /> Legend
                  </button>
                )}
              </Panel>
            </ReactFlow>
            </SquareRoutes>
            </>
          )}
          <HoverStyle edge={hoveredEdge} />
          {hoveredEdge && hoverPos && (
            <EdgeHoverCard
              edge={hoveredEdge}
              pos={hoverPos}
              host={host}
              fromName={nodeTitleById.get(hoveredEdge.source) ?? hoveredEdge.source}
              toName={nodeTitleById.get(hoveredEdge.target) ?? hoveredEdge.target}
            />
          )}
        </div>

        <Inspector selection={selection} onSelect={select} onEdit={editSelection} onClose={() => setSelection(null)} />
      </div>
      )}

      {form?.type === 'cluster' && <ClusterForm initial={clusters.find((c) => c.id === form.id) ?? null} edit onClose={closeForm} />}
      {form?.type === 'node' && (
        <NodeForm
          edit
          initial={machines.find((n) => n.id === form.id) ?? null}
          defaultClusterId={selection?.kind === 'cluster' ? selection.id : undefined}
          onClose={closeForm}
        />
      )}
      {form?.type === 'device' && <DeviceForm initial={devices.find((d) => d.id === form.id) ?? null} edit onClose={closeForm} />}
      {form?.type === 'service' && (
        <ServiceForm
          edit
          initial={services.find((w) => w.id === form.id) ?? null}
          defaultClusterId={selection?.kind === 'cluster' ? selection.id : undefined}
          onClose={closeForm}
        />
      )}
      {connect.dialogs}
      {telemetry.dialogs}
    </div>
  )
}

/** Leaves room under the graph for the legend. */
const FIT_PADDING = { top: '4%', left: '4%', right: '4%', bottom: '72px' } as const
// Fitting a big graph into the window never zooms out past 0.5: below that even the far presentation's names
// are too small to read. A graph that does not fit then runs past the window; pan or zoom out to see it all.
const FIT_MIN_ZOOM = 0.5
const FIT_VIEW_OPTIONS = { padding: FIT_PADDING, minZoom: FIT_MIN_ZOOM }
const PRO_OPTIONS = { hideAttribution: true }
// Whether the canvas has already animated its initial fitView once this session - see the effect above.
let hasEverFit = false

export default function TopologyPage() {
  return (
    <ReactFlowProvider>
      <Canvas />
    </ReactFlowProvider>
  )
}
