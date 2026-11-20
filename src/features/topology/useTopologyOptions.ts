import { useEffect, useMemo } from 'react'
import { applyFilter, DEFAULT_KINDS, encodeList, filterActive, hopNeighborhood, isFreshApplicationView, knownOnly, parseFilter } from '@/features/topology/lib/filter'
import type { GroupBy, ViewKind } from '@/lib/types'
import type { ParamChanges } from '@/lib/useQueryParams'
import type { RawState } from '@/store/topology'

/** The tabs of the page: the two drawn on the canvas, the map and the telemetry lanes. */
export type Mode = ViewKind | 'map' | 'telemetry'
export const VIEWS: { value: Mode; label: string; hint: string }[] = [
  { value: 'application', label: 'Application', hint: 'Microservices and the clusters they run in' },
  { value: 'infrastructure', label: 'Infrastructure', hint: 'Nodes (VMs / machines) grouped by cluster' },
  { value: 'map', label: 'Map', hint: 'Where your sites are in the world' },
  { value: 'telemetry', label: 'Telemetry', hint: 'The path telemetry takes from each cluster to FUSION' },
]

/** What the address says about how to draw the topology: every option, with the default for one that is not there. */
export interface ViewOptions {
  mode: Mode
  isMap: boolean
  isTelemetry: boolean
  /** Application or Infrastructure: the two tabs drawn on the canvas, with its options. */
  isCanvas: boolean
  view: ViewKind
  groupBy: GroupBy
  servicesOnNodes: boolean
  links: boolean
  showDevices: boolean
  showLabels: boolean
  showNoise: boolean
  showMesh: boolean
  showNamespaces: boolean
  showChain: boolean
  /** Recolours the measured line between two clusters by its live loss. */
  showHealthLens: boolean
  /** The Telemetry tab's own filter: only the lanes where something needs attention or is not working. */
  problemsOnly: boolean
}

/**
 * The view options as the address holds them. Nothing here is state of its own: the address is the state, so a view can be linked to and
 * a reload keeps it. An option that only means something with another (namespaces need the cluster grouping, chain the application view,
 * mesh a cluster that runs one) reads as off without it, whatever the address says.
 */
export function readViewOptions(sp: URLSearchParams, hasMesh: boolean): ViewOptions {
  const mode: Mode = VIEWS.find((v) => v.value === sp.get('view'))?.value ?? 'application'
  const isMap = mode === 'map'
  const isTelemetry = mode === 'telemetry'
  const groupBy: GroupBy = sp.get('group') === 'tier' ? 'tier' : 'cluster'
  return {
    mode,
    isMap,
    isTelemetry,
    isCanvas: !isMap && !isTelemetry,
    view: isMap || isTelemetry ? 'application' : mode,
    groupBy,
    servicesOnNodes: sp.get('services') === '1',
    links: sp.get('links') !== '0',
    showDevices: sp.get('devices') !== '0',
    showLabels: sp.get('labels') === '1',
    showNoise: sp.get('noise') === '1',
    // Asked for in the URL, but only means something when a cluster runs a mesh.
    showMesh: sp.get('mesh') === '1' && hasMesh,
    // Only means something grouped by cluster (a tier box already mixes clusters together).
    showNamespaces: sp.get('namespaces') === '1' && groupBy === 'cluster',
    // Chain lays every service out in one flat left-to-right order across every cluster, so it replaces the cluster/tier boxes and
    // namespace sub-boxes rather than combining with them.
    showChain: sp.get('chain') === '1' && mode === 'application',
    showHealthLens: sp.get('health') === '1',
    problemsOnly: isTelemetry && sp.get('problems') === '1',
  }
}

/**
 * `readViewOptions` for the page, with the one-time default for a fresh Application view.
 *
 * The Application view defaults to the services (Deployments and StatefulSets) rather than "everything": a fresh visit with no filter chosen
 * yet is the common case, and starting there with just the services makes the canvas read as actual services (not DaemonSets/Jobs) and gives
 * a topology selection fewer, more relevant cards to choose from. A one-time URL rewrite, not a render-time default - deriving it at render
 * time instead would break the moment someone unchecks the only active kind, since the resulting empty selection writes the exact same "no
 * kinds param" URL the default itself would read, so the default would silently reassert itself on the very next render. Running once, on
 * mount, means it can never re-fire after that no matter what the person does afterward (including clearing it right back to "everything").
 */
export function useViewOptions(sp: URLSearchParams, setParams: (changes: ParamChanges) => void, hasMesh: boolean): ViewOptions & { setParam: (key: string, value: string | null) => void } {
  const options = useMemo(() => readViewOptions(sp, hasMesh), [sp, hasMesh])
  useEffect(() => {
    if (isFreshApplicationView(sp)) setParams({ kinds: encodeList(DEFAULT_KINDS) })
    // only meant to run once, at first mount
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  return { ...options, setParam: (key, value) => setParams({ [key]: value }) }
}

type Topology = RawState

/**
 * What the canvas is to show: the records the cluster, application and kind filters leave (`?clusters=a,b&apps=x&kinds=…`, ids that no
 * longer exist ignored), narrowed to a neighbourhood while a service is selected and `?hops=N` is set - Backstage-style "max depth":
 * what it calls and what calls it, N steps out, independent of the filter and only in effect while that service is the selection.
 */
export function useTopologyScope(sp: URLSearchParams, topology: Topology, focusId: string | undefined) {
  const { clusters, nodes: machines, namespaces, services, devices, dependencies, applications, sites, siteLinks, externalEndpoints } = topology
  const rawFilter = useMemo(() => parseFilter(sp), [sp])
  const filter = useMemo(() => knownOnly(rawFilter, { clusters, applications }), [rawFilter, clusters, applications])
  const filtering = filterActive(filter)
  const filteredByAttrs = useMemo(
    () => applyFilter({ clusters, nodes: machines, namespaces, services, devices, dependencies, applications, sites, siteLinks, externalEndpoints }, filter),
    [clusters, machines, namespaces, services, devices, dependencies, applications, sites, siteLinks, externalEndpoints, filter],
  )
  const hopsParam = sp.get('hops')
  const hops = hopsParam !== null && /^\d+$/.test(hopsParam) ? Number(hopsParam) : undefined
  const shown = useMemo(() => (hops !== undefined && focusId ? hopNeighborhood(filteredByAttrs, focusId, hops) : filteredByAttrs), [filteredByAttrs, hops, focusId])
  const nothingMatches = filtering && shown.clusters.length === 0 && shown.devices.length === 0
  return { filter, filtering, shown, hops, nothingMatches }
}
