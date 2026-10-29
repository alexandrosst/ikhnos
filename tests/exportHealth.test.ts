import assert from 'node:assert/strict'
import { test } from 'node:test'
import type { AgentDiagnostics, AgentExportHealth, AgentExportRoute } from '../src/lib/consent'
import { droppedRecently, exportReach, exportRows, queueShare } from '../src/lib/exportHealth'

const route = (over: Partial<AgentExportRoute>): AgentExportRoute => ({
  exporter: 'otlp',
  signal: 'metrics',
  state: 'exporting',
  sent: 10,
  failed: 0,
  ...over,
})
const health = (routes: AgentExportRoute[], over: Partial<AgentExportHealth> = {}): AgentExportHealth => ({ podsReached: 2, podsFailed: 0, routes, ...over })

test('exportRows: one row per signal type that has a signal on, in metrics, logs, traces order', () => {
  const rows = exportRows(['traces', 'systemLogs', 'resourceUsage', 'energy'], health([]))
  assert.deepEqual(
    rows.map((r) => r.modality),
    ['metrics', 'logs', 'traces'],
  )
  assert.deepEqual(rows[0].signals, ['Resource usage', 'Energy'])
})

test('exportRows: a signal type with nothing installed has no row, and one with no route yet is waiting', () => {
  const rows = exportRows(['resourceUsage'], health([route({ signal: 'logs' })]))
  assert.equal(rows.length, 1)
  assert.deepEqual(
    {
      m: rows[0].modality,
      s: rows[0].state,
      e: rows[0].exporters,
      n: rows[0].sent,
    },
    { m: 'metrics', s: 'waiting', e: [], n: 0 },
  )
})

test('exportRows: each signal type reads its own route', () => {
  const rows = exportRows(
    ['resourceUsage', 'systemLogs', 'traces'],
    health([
      route({
        exporter: 'otlphttp/metrics',
        signal: 'metrics',
        state: 'exporting',
        sent: 5,
        lastSentAt: '2026-10-06T12:00:00Z',
      }),
      route({
        exporter: 'otlphttp/logs',
        signal: 'logs',
        state: 'failing',
        failed: 9,
      }),
      route({
        exporter: 'zipkin/traces',
        signal: 'traces',
        state: 'silent',
        sent: 3,
      }),
    ]),
  )
  assert.deepEqual(
    rows.map((r) => [r.modality, r.state, r.exporters]),
    [
      ['metrics', 'exporting', ['otlphttp/metrics']],
      ['logs', 'failing', ['otlphttp/logs']],
      ['traces', 'silent', ['zipkin/traces']],
    ],
  )
})

test('exportRows: several exporters for one signal type show the worst state and add up', () => {
  const [row] = exportRows(
    ['resourceUsage'],
    health([
      route({
        exporter: 'otlp',
        state: 'exporting',
        sent: 4,
        lastSentAt: '2026-10-06T12:00:00Z',
      }),
      route({
        exporter: 'otlphttp/x',
        state: 'failing',
        sent: 1,
        failed: 2,
        lastSentAt: '2026-10-06T12:05:00Z',
      }),
    ]),
  )
  assert.deepEqual(
    {
      s: row.state,
      n: row.sent,
      f: row.failed,
      e: row.exporters,
      t: row.lastSentAt,
    },
    {
      s: 'failing',
      n: 5,
      f: 2,
      e: ['otlp', 'otlphttp/x'],
      t: '2026-10-06T12:05:00Z',
    },
  )
})

const diag = (h?: AgentExportHealth) => ({ exportHealth: h }) as AgentDiagnostics
test('exportReach: not reported without the field, unreadable only when nothing was reached and something failed', () => {
  assert.equal(exportReach(undefined), 'not-reported')
  assert.equal(exportReach(diag()), 'not-reported')
  assert.equal(exportReach(diag(health([], { podsReached: 0, podsFailed: 2 }))), 'unreadable')
  assert.equal(exportReach(diag(health([], { podsReached: 1, podsFailed: 1 }))), 'ok')
  // Nothing found and nothing failed: no collectors to read, which is not a failure to read them.
  assert.equal(exportReach(diag(health([], { podsReached: 0, podsFailed: 0 }))), 'ok')
})

test('exportRows: the queues and what they refused are summed over the routes of a signal type', () => {
  const rows = exportRows(
    ['resourceUsage'],
    health([
      route({ exporter: 'otlp/a', queued: 30, queueCapacity: 100, dropped: 2, lastDroppedAt: '2026-10-06T12:00:00Z' }),
      route({ exporter: 'otlp/b', queued: 10, queueCapacity: 100, dropped: 5, lastDroppedAt: '2026-10-06T12:05:00Z' }),
    ]),
  )
  assert.deepEqual(
    { q: rows[0].queued, c: rows[0].queueCapacity, d: rows[0].dropped, at: rows[0].lastDroppedAt },
    { q: 40, c: 200, d: 7, at: '2026-10-06T12:05:00Z' },
  )
  // An older agent says none of it: zeros, not a missing field.
  const old = exportRows(['resourceUsage'], health([route({})]))
  assert.deepEqual({ q: old[0].queued, c: old[0].queueCapacity, d: old[0].dropped }, { q: 0, c: 0, d: 0 })
})

test('queueShare is how full the queues are, and nothing when the collectors did not say how much they hold', () => {
  assert.equal(queueShare({ queued: 25, queueCapacity: 100 }), 0.25)
  assert.equal(queueShare({ queued: 250, queueCapacity: 100 }), 1)
  assert.equal(queueShare({ queued: 25, queueCapacity: 0 }), undefined)
})

test('droppedRecently: only a drop in the last quarter of an hour counts as now', () => {
  const now = Date.parse('2026-10-06T12:10:00Z')
  assert.equal(droppedRecently({ dropped: 3, lastDroppedAt: '2026-10-06T12:00:00Z' }, now), true)
  assert.equal(droppedRecently({ dropped: 3, lastDroppedAt: '2026-10-06T11:40:00Z' }, now), false)
  assert.equal(droppedRecently({ dropped: 3 }, now), false)
  assert.equal(droppedRecently({ dropped: 0, lastDroppedAt: '2026-10-06T12:09:00Z' }, now), false)
})
