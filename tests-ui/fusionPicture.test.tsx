import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, test } from 'vitest'
import { FusionPicture } from '@/features/fusion/components/FusionPicture'
import { fusionPicture } from '@/features/fusion/lib/fusionPicture'
import { fusionProblems } from '@/features/fusion/lib/fusionStatus'
import type { FusionRetention, FusionStatus } from '@/lib/api'
import type { SendingHop as Hop } from '@/features/fusion/lib/fusionPicture'

const NOW = Date.parse('2026-10-01T12:00:00Z')
const LABEL = { metrics: 'Prometheus', logs: 'Loki', traces: 'Tempo', central: 'Central operator', grafana: 'Grafana' } as const
const part = (component: keyof typeof LABEL, over: object = {}) => ({ component, label: LABEL[component], desired: 1, ready: 1, ...over })
const status = (over: Partial<FusionStatus> = {}): FusionStatus => ({
  available: true, state: 'running', data: true, lastDataAt: new Date(NOW - 12_000).toISOString(),
  lastData: { metrics: new Date(NOW - 12_000).toISOString(), logs: new Date(NOW - 20_000).toISOString(), traces: new Date(NOW - 30_000).toISOString() }, since: new Date(NOW - 3600_000).toISOString(),
  components: [part('central'), part('metrics'), part('logs'), part('traces'), part('grafana')],
  central: { operatorId: 'op-central', endpoint: 'central.example:4317', exposed: true, exists: true },
  ...over,
} as FusionStatus)
const hop = (key: 'regional' | 'local', total: number, state: Hop['state'] = 'healthy', direct?: number): Hop => ({ key, label: key, total, healthy: total, state, worst: total, settingUp: 0, ...(direct !== undefined ? { direct: { total: direct, state, worst: direct } } : {}) })
const GIB = 2 ** 30
const store = (component: 'metrics' | 'logs' | 'traces', over: object = {}) => ({ component, label: LABEL[component], value: '7d', days: 7, exactDays: true, minDays: 1, maxDays: 365, volumeKnown: true, volumeBytes: 10 * GIB, capacityBytes: 10 * GIB, usedBytes: GIB, usedSource: 'volume', share: 0.3, ...over })
const retention = (...s: object[]) => ({ available: true, running: true, stores: s } as unknown as FusionRetention)

const model = (s: FusionStatus, hops: Hop[] = [hop('regional', 2)], r: FusionRetention | null = null) =>
  fusionPicture({ status: s, retention: r, problems: fusionProblems(s, r, NOW), hops, now: NOW })!
const link = (m: ReturnType<typeof model>, from: string, to: string) => m.edges.find((e) => e.from === from && e.to === to)?.link

describe('fusionPicture', () => {
  test('a FUSION that is running and receiving has every arrow flowing', () => {
    const m = model(status())
    expect(m.senders.map((n) => n.id)).toEqual(['regional'])
    expect(m.stores.map((n) => n.id)).toEqual(['metrics', 'logs', 'traces'])
    expect(m.edges.map((e) => e.link)).toEqual(Array(m.edges.length).fill('flowing'))
    expect(m.edges.map((e) => `${e.from}>${e.to}`)).toEqual(['regional>central', 'central>metrics', 'central>logs', 'central>traces', 'metrics>grafana', 'logs>grafana', 'traces>grafana'])
  })

  test('a store that is down stops its own arrows, and the others go on', () => {
    const m = model(status({ state: 'attention', components: [part('central'), part('metrics'), part('logs', { ready: 0, reason: 'Waiting for a volume' }), part('traces'), part('grafana')] }))
    expect(link(m, 'central', 'logs')).toBe('broken')
    expect(link(m, 'logs', 'grafana')).toBe('broken')
    expect(link(m, 'central', 'metrics')).toBe('flowing')
    expect(link(m, 'central', 'traces')).toBe('flowing')
    expect(link(m, 'regional', 'central')).toBe('flowing')
    expect(m.stores.find((n) => n.id === 'logs')).toMatchObject({ health: 'down', more: 'Waiting for a volume' })
  })

  test('a volume that is nearly full holds its store up without stopping it', () => {
    const r = retention(store('metrics'), store('logs', { usedBytes: 9.5 * GIB }), store('traces'))
    const m = model(status(), [hop('regional', 1)], r)
    expect(link(m, 'central', 'logs')).toBe('stalled')
    expect(m.stores.find((n) => n.id === 'logs')?.store).toBeDefined()
  })

  test('a central operator that only the cluster can reach holds up what comes from other clusters', () => {
    const m = model(status({ central: { operatorId: 'op-central', endpoint: 'central.svc:4317', exposed: false, exists: true } }))
    expect(link(m, 'regional', 'central')).toBe('stalled')
    expect(m.central.more).toContain('Inside this cluster only')
  })

  test('no data for a while is not drawn as moving', () => {
    const old = new Date(NOW - 20 * 60_000).toISOString()
    const m = model(status({ lastDataAt: old, lastData: { metrics: old, logs: old, traces: old } }))
    expect(link(m, 'central', 'metrics')).toBe('unknown')
    expect(link(m, 'regional', 'central')).toBe('unknown')
    expect(link(m, 'metrics', 'grafana')).toBe('flowing')
  })

  test('each store is drawn as receiving only on its own word: one that is quiet while the others receive is not', () => {
    const m = model(status({ lastData: { metrics: new Date(NOW - 5_000).toISOString(), traces: new Date(NOW - 2 * 3600_000).toISOString() } }))
    expect(link(m, 'central', 'metrics')).toBe('flowing')
    expect(link(m, 'central', 'logs')).toBe('unknown')
    expect(link(m, 'central', 'traces')).toBe('unknown')
    expect(link(m, 'regional', 'central')).toBe('flowing')
    expect(m.stores.find((n) => n.id === 'logs')?.more).toBe('No log line in the last six hours')
    expect(m.stores.find((n) => n.id === 'traces')?.more).toBe('Last trace 2 h ago')
    expect(m.stores.find((n) => n.id === 'metrics')?.more).toBe('Last sample just now')
  })

  test('a server that only knows the metrics time says nothing of the logs and traces', () => {
    const m = model(status({ lastData: undefined }))
    expect(link(m, 'central', 'metrics')).toBe('flowing')
    expect(link(m, 'central', 'logs')).toBe('unknown')
    expect(m.stores.find((n) => n.id === 'logs')?.more).toBeUndefined()
  })

  test('local operators are drawn only where some send straight to it, and nothing sending says so', () => {
    expect(model(status(), [hop('local', 3, 'healthy', 0)]).senders.map((n) => n.id)).toEqual(['none'])
    const m = model(status(), [hop('regional', 1), hop('local', 3, 'healthy', 2)])
    expect(m.senders.map((n) => n.id)).toEqual(['regional', 'local'])
    expect(m.senders[1].note).toBe('2 operators send straight here')
    expect(model(status(), [hop('regional', 1), hop('local', 3, 'healthy', 1)]).senders[1].note).toBe('1 operator sends straight here')
  })

  test('a failing sender stops its arrow into the central operator', () => {
    expect(link(model(status(), [hop('regional', 2, 'down')]), 'regional', 'central')).toBe('broken')
    expect(link(model(status(), [hop('regional', 2, 'attention')]), 'regional', 'central')).toBe('stalled')
  })

  test('while it starts, nothing is claimed to move; off and unavailable draw nothing', () => {
    const m = model(status({ state: 'starting', lastDataAt: undefined, components: [part('central', { ready: 0 }), part('metrics'), part('logs', { ready: 0 }), part('traces', { ready: 0 }), part('grafana', { ready: 0 })] }))
    expect(m.edges.every((e) => e.link === 'unknown')).toBe(true)
    expect(m.central.health).toBe('starting')
    expect(fusionPicture({ status: status({ state: 'off' }), retention: null, problems: [], hops: [], now: NOW })).toBeNull()
    expect(fusionPicture({ status: status({ available: false }), retention: null, problems: [], hops: [], now: NOW })).toBeNull()
  })

  test('Grafana is left out when it is not part of the release', () => {
    const m = model(status({ components: [part('central'), part('metrics'), part('logs'), part('traces')] }))
    expect(m.grafana).toBeUndefined()
    expect(m.edges.some((e) => e.to === 'grafana')).toBe(false)
  })
})

describe('FusionPicture', () => {
  test('every box says its state in words, and each arrow carries its link', () => {
    render(<MemoryRouter><FusionPicture model={model(status({ state: 'attention', components: [part('central'), part('metrics'), part('logs', { ready: 0 }), part('traces'), part('grafana')] }))} /></MemoryRouter>)
    expect(screen.getByTestId('fusion-node-logs')).toHaveAttribute('data-health', 'down')
    expect(screen.getByTestId('fusion-node-logs')).toHaveTextContent('Not working')
    expect(screen.getByTestId('fusion-node-metrics')).toHaveTextContent('Healthy')
    expect(screen.getByTestId('fusion-edge-central-logs')).toHaveAttribute('data-link', 'broken')
    expect(screen.getByTestId('fusion-edge-central-metrics')).toHaveAttribute('data-link', 'flowing')
    expect(screen.getByRole('link', { name: 'Regional operators' })).toHaveAttribute('href', '/pipeline')
  })
})
