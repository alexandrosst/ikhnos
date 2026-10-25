import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import LocalOperatorPicture from '@/features/pipeline/components/telemetry/LocalOperatorPicture'
import { operatorPicture } from '@/features/pipeline/lib/operatorPicture'
import type { AgentDiagnostics, AgentExportHealth } from '@/lib/consent'

const NOW = Date.parse('2026-10-06T12:10:00Z')

function diag(h: AgentExportHealth | undefined, installed: string[], endpoint = 'otel.example.com:4317'): AgentDiagnostics {
  if (h && !h.sources) h = { ...h, sources: [] }
  return {
    reportedAt: '2026-10-06T12:09:00Z', agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0,
    collectors: [], informers: [], problems: [], installedTelemetry: installed, exportHealth: h,
    installedTelemetryConfig: { exportEndpoint: endpoint, redactionEnabled: false, resourceDetectionEnabled: false },
  } as unknown as AgentDiagnostics
}
const healthy: AgentExportHealth = { sources: [], podsReached: 4, podsFailed: 0, routes: [{ exporter: 'otlp', signal: 'metrics', state: 'exporting', sent: 9, failed: 0, lastSentAt: '2026-10-06T12:09:40Z' }] }
const failing: AgentExportHealth = { sources: [], podsReached: 4, podsFailed: 0, routes: [{ exporter: 'otlp', signal: 'metrics', state: 'failing', sent: 0, failed: 5, lastFailedAt: '2026-10-06T12:09:40Z' }] }
const show = (d: AgentDiagnostics | undefined, installed: string[], extra: Record<string, unknown> = {}) =>
  render(<LocalOperatorPicture installed={installed} diagnostics={d} cluster="vradipus" now={NOW} {...extra} />)

describe('operatorPicture: the local operator from what the agent reports', () => {
  test('sources are grouped by where they run, only the ones that are on', () => {
    const p = operatorPicture({ installed: ['resourceUsage', 'kubernetesEvents', 'traces'], diagnostics: diag(healthy, ['resourceUsage']), now: NOW, cluster: 'c' })
    expect(p.groups.map((g) => [g.scope, g.sources.map((s) => s.id)])).toEqual([
      ['node', ['resourceUsage']],
      ['cluster', ['kubernetesEvents']],
      ['application', ['traces']],
    ])
    expect(p.summary).toBe('3 sources · metrics, logs, traces · to otel.example.com:4317')
  })

  test('its state is the one "Is data arriving?" gives, and a problem asks to be looked at', () => {
    const ok = operatorPicture({ installed: ['resourceUsage'], diagnostics: diag(healthy, ['resourceUsage']), now: NOW, cluster: 'c' })
    expect(ok.health).toBe('healthy')
    expect(ok.needsAttention).toBe(false)
    const bad = operatorPicture({ installed: ['resourceUsage'], diagnostics: diag(failing, ['resourceUsage']), now: NOW, cluster: 'c' })
    expect(bad.health).toBe('down')
    expect(bad.needsAttention).toBe(true)
  })

  test('counters that cannot be read, or collector pods that failed to be read, are said at the collectors', () => {
    const unreadable = operatorPicture({ installed: ['resourceUsage'], diagnostics: diag({ sources: [], receivers: [], podsReached: 0, podsFailed: 3, lastError: 'no such host', routes: [] }, ['resourceUsage']), now: NOW, cluster: 'c' })
    expect(unreadable.collectors.text).toBe('The agent cannot read their counters')
    expect(unreadable.needsAttention).toBe(true)
    const partial = operatorPicture({ installed: ['resourceUsage'], diagnostics: diag({ ...healthy, podsReached: 2, podsFailed: 1 }, ['resourceUsage']), now: NOW, cluster: 'c' })
    expect(partial.collectors.text).toBe('2 collector pods read, 1 could not be read')
  })

  test('an agent that reports nothing is unknown, which is not a fault', () => {
    const p = operatorPicture({ installed: ['resourceUsage'], diagnostics: undefined, now: NOW, cluster: 'c' })
    expect(p.health).toBe('unknown')
    expect(p.needsAttention).toBe(false)
    expect(p.collectors.text).toBe('The agent has not reported yet')
  })
})

describe('LocalOperatorPicture', () => {
  test('is closed while all is well, with one line to say what it is, and opens on a click', () => {
    show(diag(healthy, ['resourceUsage']), ['resourceUsage'])
    expect(screen.getByTestId('operator-picture')).toHaveAttribute('data-open', 'false')
    expect(screen.getByTestId('operator-picture-summary')).toHaveTextContent('1 source · metrics · to otel.example.com:4317')
    expect(screen.queryByTestId('operator-picture-body')).toBeNull()
    fireEvent.click(screen.getByTestId('operator-picture-toggle'))
    expect(screen.getByTestId('operator-picture-source-resourceUsage')).toBeInTheDocument()
    expect(screen.getByTestId('operator-picture-lane-metrics')).toHaveAttribute('data-health', 'healthy')
    expect(screen.getByTestId('operator-picture-collectors-text')).toHaveTextContent('4 collector pods read')
    expect(screen.getByTestId('operator-picture-destination')).toHaveTextContent('otel.example.com:4317')
  })

  test('opens by itself when something along the path is failing, and a click to close it is kept', () => {
    const { rerender } = show(diag(healthy, ['resourceUsage']), ['resourceUsage'])
    expect(screen.getByTestId('operator-picture')).toHaveAttribute('data-open', 'false')
    rerender(<LocalOperatorPicture installed={['resourceUsage']} diagnostics={diag(failing, ['resourceUsage'])} cluster="vradipus" now={NOW} />)
    expect(screen.getByTestId('operator-picture')).toHaveAttribute('data-open', 'true')
    expect(screen.getByTestId('operator-picture-lane-metrics')).toHaveAttribute('data-health', 'down')
    expect(screen.getByTestId('operator-picture-lane-metrics')).toHaveTextContent(/5 sends failed/)
    fireEvent.click(screen.getByTestId('operator-picture-toggle'))
    expect(screen.getByTestId('operator-picture')).toHaveAttribute('data-open', 'false')
    rerender(<LocalOperatorPicture installed={['resourceUsage']} diagnostics={diag(failing, ['resourceUsage'])} cluster="vradipus" now={NOW + 60_000} />)
    expect(screen.getByTestId('operator-picture')).toHaveAttribute('data-open', 'false')
  })

  test('says that the tool behind a source is the owner\'s, and who keeps the release', () => {
    show(diag(healthy, ['resourceUsage']), ['resourceUsage'], { keptByController: true })
    fireEvent.click(screen.getByTestId('operator-picture-toggle'))
    expect(screen.getByTestId('operator-picture-note')).toHaveTextContent(/Kept by the Ikhnos controller.*tool reads each source is the cluster owner/)
  })

  test('with nothing collected there is nothing to draw, and it says how to start', () => {
    show(undefined, [])
    fireEvent.click(screen.getByTestId('operator-picture-toggle'))
    expect(screen.getByTestId('operator-picture-body')).toHaveTextContent(/No telemetry signal is enabled/)
  })
})

const AT = '2026-10-06T12:09:30Z'
const kubelet = (state: 'failing' | 'degraded' | 'receiving' | 'silent' | 'waiting'): AgentExportHealth => ({
  ...healthy,
  receivers: [],
  sources: [{ receiver: 'kubelet_stats', signals: ['resourceUsage', 'nodeRuntime'], state, accepted: 100, failed: 12, lastAcceptedAt: '2026-10-06T11:00:00Z', lastFailedAt: AT }],
})

describe('a source that is not being read', () => {
  test('is told per source, for each signal its receiver serves, while the data that does flow stays healthy', () => {
    const p = operatorPicture({ installed: ['resourceUsage', 'nodeRuntime', 'kubernetesEvents'], diagnostics: diag(kubelet('failing'), ['resourceUsage', 'nodeRuntime', 'kubernetesEvents']), now: NOW, cluster: 'c' })
    const node = p.groups.find((g) => g.scope === 'node')!.sources
    expect(node.map((s) => [s.id, s.read?.state])).toEqual([['resourceUsage', 'failing'], ['nodeRuntime', 'failing']])
    expect(node[0].read?.sentence).toMatch(/12 values could not be read \(the last 30 s ago\) and nothing has come in since.*insecureSkipVerify/)
    // The events have no receiver in the report: nothing is claimed about how they are read.
    expect(p.groups.find((g) => g.scope === 'cluster')!.sources[0].read).toBeUndefined()
    expect(p.health).toBe('attention')
    expect(p.needsAttention).toBe(true)
    expect(p.summary).toMatch(/· 2 to look at$/)
  })

  test('partly read is shown but does not raise the picture, and a quiet or waiting source is not a fault', () => {
    for (const state of ['degraded', 'silent', 'waiting'] as const) {
      const p = operatorPicture({ installed: ['resourceUsage'], diagnostics: diag(kubelet(state), ['resourceUsage']), now: NOW, cluster: 'c' })
      expect(p.groups[0].sources[0].read?.state).toBe(state)
      expect(p.needsAttention).toBe(false)
    }
  })

  test('the worst of the receivers that serve one signal is the signal\'s', () => {
    const h: AgentExportHealth = { ...healthy, receivers: [], sources: [
      { receiver: 'hostmetrics', signals: ['resourceUsage'], state: 'receiving', accepted: 9, failed: 0 },
      { receiver: 'kubelet_stats', signals: ['resourceUsage', 'nodeRuntime'], state: 'failing', accepted: 9, failed: 3, lastFailedAt: AT },
    ] }
    const p = operatorPicture({ installed: ['resourceUsage', 'nodeRuntime'], diagnostics: diag(h, ['resourceUsage', 'nodeRuntime']), now: NOW, cluster: 'c' })
    expect(p.groups[0].sources.map((s) => s.read?.state)).toEqual(['failing', 'failing'])
  })

  test('the picture shows it, and opens by itself', () => {
    show(diag(kubelet('failing'), ['resourceUsage']), ['resourceUsage'])
    expect(screen.getByTestId('operator-picture')).toHaveAttribute('data-open', 'true')
    expect(screen.getByTestId('operator-picture-source-resourceUsage')).toHaveAttribute('data-read', 'failing')
    expect(screen.getByTestId('operator-picture-source-resourceUsage-state')).toHaveTextContent('Not reading')
    expect(screen.getByTestId('operator-picture-source-resourceUsage-why')).toHaveTextContent(/12 values could not be read/)
  })
})

const tool = (state: 'failing' | 'degraded' | 'receiving', targets: number, targetsUp: number, down: string[]): AgentExportHealth => ({
  ...healthy,
  receivers: [],
  sources: [{ receiver: 'kepler', signals: ['energy'], state, accepted: 0, failed: 0, targets, targetsUp, down }],
})

describe('a tool the collector scrapes', () => {
  test('answering on only some of its nodes is told with the nodes that do not, and needs a look', () => {
    const p = operatorPicture({ installed: ['energy'], diagnostics: diag(tool('degraded', 3, 2, ['node-b']), ['energy']), now: NOW, cluster: 'c' })
    const r = p.groups[0].sources[0].read!
    expect(r.state).toBe('degraded')
    expect(r.health).toBe('attention')
    expect(r.sentence).toMatch(/^It answered the collector with data on 2 of 3\. Not answering: node-b\. Look at the Kepler pods/)
    expect(p.needsAttention).toBe(true)
    expect(p.summary).toMatch(/· 1 to look at$/)
  })

  test('answering nowhere is said as such, and names the nodes it can, and "and others" when the list was cut', () => {
    const p = operatorPicture({ installed: ['energy'], diagnostics: diag(tool('failing', 12, 0, ['a', 'b']), ['energy']), now: NOW, cluster: 'c' })
    expect(p.groups[0].sources[0].read?.sentence).toMatch(/^None of its 12 targets answered the collector with data\. Not answering: a, b and others\./)
    const one = operatorPicture({ installed: ['energy'], diagnostics: diag(tool('failing', 1, 0, ['a']), ['energy']), now: NOW, cluster: 'c' })
    expect(one.groups[0].sources[0].read?.sentence).toMatch(/^None of its 1 target answered the collector with data\. Not answering: a\./)
  })

  test('answering on all of its nodes is not a fault', () => {
    const p = operatorPicture({ installed: ['energy'], diagnostics: diag(tool('receiving', 3, 3, []), ['energy']), now: NOW, cluster: 'c' })
    expect(p.groups[0].sources[0].read?.health).toBe('healthy')
    expect(p.needsAttention).toBe(false)
  })

  test('the picture opens by itself and shows the word and the why', () => {
    show(diag(tool('degraded', 3, 2, ['node-b']), ['energy']), ['energy'])
    expect(screen.getByTestId('operator-picture')).toHaveAttribute('data-open', 'true')
    expect(screen.getByTestId('operator-picture-source-energy')).toHaveAttribute('data-read', 'degraded')
    expect(screen.getByTestId('operator-picture-source-energy-state')).toHaveTextContent('Partly reading')
    expect(screen.getByTestId('operator-picture-source-energy-why')).toHaveTextContent(/Not answering: node-b/)
  })
})

const wl = (over: Partial<import('@/lib/generated/wire').WorkloadDoc>) => ({ name: 'ikhnos-telemetry-kepler', kind: 'DaemonSet', component: 'telemetry-kepler', wanted: 3, ready: 3, ...over })
const withWorkloads = (d: AgentDiagnostics, workloads: ReturnType<typeof wl>[]): AgentDiagnostics => ({ ...d, workloads })

describe('the pods of a tool, where a controller keeps the release', () => {
  test('a pod held back on a node is said at the tool, and asks to be looked at, though no scrape target exists for it', () => {
    const d = withWorkloads(diag(healthy, ['energy']), [wl({ ready: 2, notReadySeconds: 720 })])
    const p = operatorPicture({ installed: ['energy'], diagnostics: d, now: NOW, cluster: 'c' })
    const r = p.groups[0].sources[0].read!
    expect(r.state).toBe('degraded')
    expect(r.health).toBe('attention')
    expect(r.sentence).toMatch(/^Kepler runs on 2 of the 3 nodes it is asked to, for 12 min now\. A node it cannot run on, a taint/)
    expect(p.needsAttention).toBe(true)
  })

  test('none of the pods ready is not reading, and a tool that is not in the cluster says nothing', () => {
    const none = operatorPicture({ installed: ['energy'], diagnostics: withWorkloads(diag(healthy, ['energy']), [wl({ ready: 0, notReadySeconds: 600 })]), now: NOW, cluster: 'c' })
    expect(none.groups[0].sources[0].read?.state).toBe('failing')
    expect(none.groups[0].sources[0].read?.sentence).toMatch(/^None of Kepler's 3 pods is ready/)
    // The accelerators' tool is not Kepler's: its pods say nothing of energy.
    const other = operatorPicture({ installed: ['energy'], diagnostics: withWorkloads(diag(healthy, ['energy']), [wl({ component: 'telemetry-dcgm', ready: 0, notReadySeconds: 600 })]), now: NOW, cluster: 'c' })
    expect(other.groups[0].sources[0].read).toBeUndefined()
  })

  test('pods that are only starting, or being rolled out, are not a fault and do not make a source that is read less read', () => {
    const starting = operatorPicture({ installed: ['energy'], diagnostics: withWorkloads(diag(healthy, ['energy']), [wl({ ready: 2, notReadySeconds: 40 })]), now: NOW, cluster: 'c' })
    expect(starting.groups[0].sources[0].read).toMatchObject({ state: 'waiting', health: 'unknown' })
    expect(starting.needsAttention).toBe(false)
    const rolling = operatorPicture({ installed: ['energy'], diagnostics: withWorkloads(diag(healthy, ['energy']), [wl({ ready: 2, rollingOut: true, notReadySeconds: 900 })]), now: NOW, cluster: 'c' })
    expect(rolling.groups[0].sources[0].read?.health).toBe('unknown')
    // Answering on both nodes that run it, the third pod starting: still being read.
    const d = withWorkloads(diag(tool('receiving', 2, 2, []), ['energy']), [wl({ ready: 2, notReadySeconds: 40 })])
    const read = operatorPicture({ installed: ['energy'], diagnostics: d, now: NOW, cluster: 'c' }).groups[0].sources[0].read!
    expect(read.state).toBe('receiving')
    expect(read.sentence).toBe('Kepler: 2 of 3 pods are ready, the others are starting.')
  })

  test('all pods ready adds nothing, and the scrape and the pods are both said when both are wrong', () => {
    const ok = operatorPicture({ installed: ['energy'], diagnostics: withWorkloads(diag(tool('receiving', 3, 3, []), ['energy']), [wl({})]), now: NOW, cluster: 'c' })
    expect(ok.groups[0].sources[0].read?.sentence).toBeUndefined()
    const both = operatorPicture({ installed: ['energy'], diagnostics: withWorkloads(diag(tool('degraded', 2, 1, ['node-b']), ['energy']), [wl({ ready: 2, notReadySeconds: 600 })]), now: NOW, cluster: 'c' })
    const s = both.groups[0].sources[0].read!.sentence!
    expect(s).toMatch(/Not answering: node-b/)
    expect(s).toMatch(/Kepler runs on 2 of the 3 nodes/)
  })

  test('the collectors say a pod of theirs that is missing, which their counters cannot', () => {
    const base = diag(healthy, ['resourceUsage'])
    const host = { name: 'ikhnos-telemetry-host', kind: 'DaemonSet', component: 'telemetry-host', wanted: 3, ready: 2 }
    const held = operatorPicture({ installed: ['resourceUsage'], diagnostics: withWorkloads(base, [{ ...host, notReadySeconds: 600 }]), now: NOW, cluster: 'c' })
    expect(held.collectors.text).toBe('4 collector pods read · node collectors: 2 of 3 pods ready for 10 min')
    expect(held.collectors.health).toBe('attention')
    expect(held.needsAttention).toBe(true)
    const starting = operatorPicture({ installed: ['resourceUsage'], diagnostics: withWorkloads(base, [{ ...host, notReadySeconds: 20 }]), now: NOW, cluster: 'c' })
    expect(starting.collectors.health).toBe('healthy')
    expect(starting.needsAttention).toBe(false)
  })
})
