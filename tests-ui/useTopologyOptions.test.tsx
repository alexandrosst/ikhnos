import { act, renderHook } from '@testing-library/react'
import type { ReactNode } from 'react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, test } from 'vitest'
import { readViewOptions, useTopologyScope, useViewOptions } from '@/features/topology/useTopologyOptions'
import { useQueryParams } from '@/lib/useQueryParams'
import type { RawState } from '@/store/topology'

const sp = (q: string) => new URLSearchParams(q)

describe('readViewOptions', () => {
  test('an empty address is the Application view, cluster boxes, links and devices on, the rest off', () => {
    expect(readViewOptions(sp(''), false)).toEqual({
      mode: 'application', isMap: false, isTelemetry: false, isCanvas: true, view: 'application', groupBy: 'cluster',
      servicesOnNodes: false, links: true, showDevices: true, showLabels: false, showNoise: false, showMesh: false, showNamespaces: false,
      showChain: false, showHealthLens: false, problemsOnly: false,
    })
  })
  test('the tabs: infrastructure is drawn on the canvas, the map and telemetry are not, and an unknown one is the default', () => {
    expect(readViewOptions(sp('view=infrastructure'), false)).toMatchObject({ mode: 'infrastructure', view: 'infrastructure', isCanvas: true })
    expect(readViewOptions(sp('view=map'), false)).toMatchObject({ mode: 'map', isMap: true, isCanvas: false, view: 'application' })
    expect(readViewOptions(sp('view=telemetry'), false)).toMatchObject({ mode: 'telemetry', isTelemetry: true, isCanvas: false })
    expect(readViewOptions(sp('view=nonsense'), false).mode).toBe('application')
  })
  test('what is switched off by writing 0, and what on by writing 1', () => {
    const o = readViewOptions(sp('links=0&devices=0&labels=1&noise=1&services=1&health=1'), false)
    expect([o.links, o.showDevices, o.showLabels, o.showNoise, o.servicesOnNodes, o.showHealthLens]).toEqual([false, false, true, true, true, true])
  })
  test('an option that needs another reads as off without it', () => {
    expect(readViewOptions(sp('mesh=1'), false).showMesh).toBe(false) // no cluster runs a mesh
    expect(readViewOptions(sp('mesh=1'), true).showMesh).toBe(true)
    expect(readViewOptions(sp('namespaces=1&group=tier'), false).showNamespaces).toBe(false) // a tier box mixes clusters
    expect(readViewOptions(sp('namespaces=1'), false).showNamespaces).toBe(true)
    expect(readViewOptions(sp('chain=1&view=infrastructure'), false).showChain).toBe(false) // chain is for services
    expect(readViewOptions(sp('chain=1'), false).showChain).toBe(true)
    expect(readViewOptions(sp('problems=1'), false).problemsOnly).toBe(false) // the telemetry tab's own filter
    expect(readViewOptions(sp('view=telemetry&problems=1'), false).problemsOnly).toBe(true)
  })
})

const at = (url: string) => ({ children }: { children: ReactNode }) => <MemoryRouter initialEntries={[url]}>{children}</MemoryRouter>

describe('useViewOptions', () => {
  const useBoth = () => {
    const [params, setParams] = useQueryParams()
    return { params, setParams, options: useViewOptions(params, setParams, false) }
  }
  test('a fresh Application view is given the services as its kinds, once', () => {
    const { result } = renderHook(useBoth, { wrapper: at('/topology') })
    expect(result.current.params.get('kinds')).toBe('Deployment,StatefulSet')
    // Cleared back to "everything" by the person: it does not come back.
    act(() => result.current.setParams({ kinds: null }))
    expect(result.current.params.has('kinds')).toBe(false)
  })
  test('a view that is not fresh is left alone', () => {
    for (const url of ['/topology?view=infrastructure', '/topology?view=map', '/topology?clusters=a', '/topology?kinds=Job']) {
      const { result } = renderHook(useBoth, { wrapper: at(url) })
      expect(result.current.params.get('kinds')).toBe(url.endsWith('kinds=Job') ? 'Job' : null)
    }
  })
  test('setParam writes one option, and null takes it out', () => {
    const { result } = renderHook(useBoth, { wrapper: at('/topology?view=infrastructure') })
    act(() => result.current.options.setParam('labels', '1'))
    expect(result.current.options.showLabels).toBe(true)
    act(() => result.current.options.setParam('labels', null))
    expect(result.current.options.showLabels).toBe(false)
  })
})

const empty = { clusters: [], nodes: [], namespaces: [], services: [], devices: [], dependencies: [], applications: [], sites: [], siteLinks: [], externalEndpoints: [] }
const svc = (id: string, clusterId: string) => ({ id, clusterId, name: id, namespace: 'ns', kind: 'Deployment' })
const topo = {
  ...empty,
  clusters: [{ id: 'c1', name: 'one', tier: 'edge' }, { id: 'c2', name: 'two', tier: 'cloud' }],
  services: [svc('a', 'c1'), svc('b', 'c1'), svc('c', 'c2'), svc('d', 'c2')],
  dependencies: [{ id: 'd1', from: 'a', to: 'b' }, { id: 'd2', from: 'b', to: 'c' }, { id: 'd3', from: 'c', to: 'd' }],
} as unknown as RawState

describe('useTopologyScope', () => {
  const scope = (q: string, focus?: string) => renderHook(() => useTopologyScope(sp(q), topo, focus)).result.current
  test('no filter shows everything and is not filtering', () => {
    const s = scope('')
    expect(s.filter).toEqual({ clusters: [], apps: [], kinds: [] })
    expect(s.shown.services).toHaveLength(4)
    expect(s.nothingMatches).toBe(false)
  })
  test('a cluster filter keeps its services, and an id that no longer exists is ignored rather than filtering everything away', () => {
    expect(scope('clusters=c2').shown.services.map((x) => x.id)).toEqual(['c', 'd'])
    expect(scope('clusters=gone').shown.services).toHaveLength(4)
  })
  test('hops narrow to a neighbourhood only while a service is selected', () => {
    expect(scope('hops=1', 'a').shown.services.map((x) => x.id).sort()).toEqual(['a', 'b'])
    expect(scope('hops=1', 'a').hops).toBe(1)
    expect(scope('hops=1').shown.services).toHaveLength(4) // nothing selected: the setting waits
    expect(scope('hops=x', 'a').hops).toBeUndefined() // not a number: ignored
    expect(scope('hops=x', 'a').shown.services).toHaveLength(4)
  })
})
