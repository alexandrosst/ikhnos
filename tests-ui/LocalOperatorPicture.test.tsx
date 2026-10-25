import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import LocalOperatorPicture from '@/features/pipeline/components/telemetry/LocalOperatorPicture'
import { operatorPicture } from '@/features/pipeline/lib/operatorPicture'
import type { AgentDiagnostics, AgentExportHealth } from '@/lib/consent'

const NOW = Date.parse('2026-10-06T12:10:00Z')

function diag(h: AgentExportHealth | undefined, installed: string[], endpoint = 'otel.example.com:4317'): AgentDiagnostics {
  return {
    reportedAt: '2026-10-06T12:09:00Z', agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0,
    collectors: [], informers: [], problems: [], installedTelemetry: installed, exportHealth: h,
    installedTelemetryConfig: { exportEndpoint: endpoint, redactionEnabled: false, resourceDetectionEnabled: false },
  } as unknown as AgentDiagnostics
}
const healthy: AgentExportHealth = { podsReached: 4, podsFailed: 0, routes: [{ exporter: 'otlp', signal: 'metrics', state: 'exporting', sent: 9, failed: 0, lastSentAt: '2026-10-06T12:09:40Z' }] }
const failing: AgentExportHealth = { podsReached: 4, podsFailed: 0, routes: [{ exporter: 'otlp', signal: 'metrics', state: 'failing', sent: 0, failed: 5, lastFailedAt: '2026-10-06T12:09:40Z' }] }
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
    const unreadable = operatorPicture({ installed: ['resourceUsage'], diagnostics: diag({ podsReached: 0, podsFailed: 3, lastError: 'no such host', routes: [] }, ['resourceUsage']), now: NOW, cluster: 'c' })
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
