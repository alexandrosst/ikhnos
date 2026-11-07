import { describe, expect, test } from 'vitest'
import { mergeDiscovered, normalizeServerState, type ServerAgent } from '@/lib/discovered'
import type { Model } from '@/lib/types'

const empty = { clusters: [], nodes: [], namespaces: [], services: [], devices: [], dependencies: [], applications: [], sites: [], siteLinks: [], externalEndpoints: [], agents: [], suggestions: [], tombstones: [], auditLog: [], refs: {}, declared: {}, declaredAgents: [] } as unknown as Model

const agent = (over: Partial<ServerAgent> = {}) =>
  ({ id: 'ag-1', orgId: 'o', name: 'atlas-agent', clusterId: 'c1', version: '1', accessTier: 2, installedTier: 2, tierCap: 3, status: 'approved', fingerprint: 'f', lateAfterSeconds: 60, requestedAt: '2026-10-01T00:00:00Z', connected: true, synced: true, modules: [], ...over }) as ServerAgent

describe('what the app keeps of an agent the server describes', () => {
  test('the overlay network its nodes are on, and the clusters that share it, reach the app', () => {
    const overlay = { product: 'tailscale', interface: 'tailscale0', kind: 'tun', range: '100.64.1.0/24', nodes: 2, ofNodes: 2, why: ['x'] }
    const doc = normalizeServerState({ agents: [agent({ overlay, overlayPeers: [{ clusterId: 'c2', maybe: false }] })] })
    const out = mergeDiscovered(empty, doc)
    expect(out.agents?.[0].overlay).toEqual(overlay)
    expect(out.agents?.[0].overlayPeers).toEqual([{ clusterId: 'c2', maybe: false }])
  })

  test('an agent with none has no overlay and an empty list, never undefined', () => {
    const out = mergeDiscovered(empty, normalizeServerState({ agents: [agent()] }))
    expect(out.agents?.[0].overlay).toBeUndefined()
    expect(out.agents?.[0].overlayPeers).toEqual([])
  })
})
