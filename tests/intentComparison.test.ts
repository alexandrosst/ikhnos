import assert from 'node:assert/strict'
import { test } from 'node:test'
import { compareIntent, differenceSentence } from '../src/features/pipeline/lib/intentComparison'
import { intentScope } from '../src/features/pipeline/lib/operatorIntent'
import { emptyTelemetry } from '../src/lib/install'
import type { TelemetryIntent } from '../src/lib/types'

const intent = (over: Partial<TelemetryIntent> = {}): TelemetryIntent => ({
  id: 'i1', agentId: 'a1', name: 'n', status: 'active', namespaces: [], exclude: [],
  signals: [{ id: 'resourceUsage', source: 'builtin' }, { id: 'systemLogs', source: 'builtin' }],
  destination: { kind: 'external', endpoint: 'gw.example.com:4317' },
  createdAt: '2026-10-01T00:00:00Z', createdBy: 'alice', ...over,
}) as TelemetryIntent

const config = (over: Record<string, unknown> = {}) => ({ exportEndpoint: 'gw.example.com:4317', redactionEnabled: true, resourceDetectionEnabled: true, ...over }) as never
const both = ['resourceUsage', 'systemLogs']

test('compareIntent: nothing to say without an intent, with nothing installed, or when the two agree', () => {
  assert.deepEqual(compareIntent(undefined, both, config()), [])
  assert.deepEqual(compareIntent(intent(), [], config()), [])
  assert.deepEqual(compareIntent(intent(), both, config()), [])
  // An agent that reports no configuration is compared on its signals alone.
  assert.deepEqual(compareIntent(intent(), both, undefined), [])
})

test('compareIntent: a signal asked for and not installed, and one installed and not asked for, are each said', () => {
  const asked = intent({ signals: [{ id: 'resourceUsage', source: 'builtin' }, { id: 'traces', source: 'builtin' }, { id: 'energy', source: 'bundle-kepler' }] as never })
  const d = compareIntent(asked, ['resourceUsage', 'systemLogs'], undefined)
  assert.deepEqual(d.map((x) => x.text), ['Asked for Traces, Energy, which are not installed', 'Running System logs, which was not asked for'])
  assert.equal(differenceSentence(d), 'Asked for Traces, Energy, which are not installed. Running System logs, which was not asked for.')
  // A signal the chart has nothing that emits is on neither side's account.
  assert.deepEqual(compareIntent(intent(), [...both, 'networkLatency'], undefined), [])
})

test('compareIntent: the destination, with the operator receiver or the dialled address accepted for an operator', () => {
  const diff = compareIntent(intent(), both, config({ exportEndpoint: 'old.example.com:4317' }))
  assert.deepEqual(diff.map((x) => [x.part, x.text]), [['destination', 'Sends everything to old.example.com:4317; asked for gw.example.com:4317']])
  // The scheme, the case and a trailing slash are not a difference.
  assert.deepEqual(compareIntent(intent(), both, config({ exportEndpoint: 'https://GW.example.com:4317/' })), [])
  const op = intent({ destination: { kind: 'operator', endpoint: 'op-eu.ikhnos-system.svc:4317', targetOperatorId: 'op-eu', address: '10.0.0.5:4317' } as never })
  assert.deepEqual(compareIntent(op, both, config({ exportEndpoint: 'op-eu.ikhnos-system.svc:4317' })), [])
  assert.deepEqual(compareIntent(op, both, config({ exportEndpoint: '10.0.0.5:4317' })), [])
  assert.equal(compareIntent(op, both, config({ exportEndpoint: '10.0.0.9:4317' }))[0].text, 'Sends everything to 10.0.0.9:4317; asked for op-eu (10.0.0.5:4317)')
})

test('compareIntent: an operator reached at the address recorded for it is the operator asked for, whichever cluster the install dials from', () => {
  // The case that was reported: the central operator, installed from another cluster at the address FUSION is exposed at. The intent names the
  // operator and no address of the sender's own, so the command dials the operator's advertised address, not its name in its own cluster.
  const central = intent({ destination: { kind: 'operator', endpoint: '', targetOperatorId: 'op-central' } as never })
  const known = [{ id: 'op-central', address: '147.102.13.108:4317', endpoint: '147.102.13.108:4317' }]
  assert.deepEqual(compareIntent(central, both, config({ exportEndpoint: '147.102.13.108:4317' }), known), [])
  // Its name in its own cluster stays accepted, and so does the address with a scheme.
  assert.deepEqual(compareIntent(central, both, config({ exportEndpoint: 'op-central.ikhnos-system.svc:4317' }), known), [])
  assert.deepEqual(compareIntent(central, both, config({ exportEndpoint: 'https://147.102.13.108:4317' }), known), [])
  // Another address is still a difference, and the sentence names the address the command would dial, not the in-cluster name.
  const d = compareIntent(central, both, config({ exportEndpoint: '10.9.9.9:4317' }), known)
  assert.equal(d[0].text, 'Sends everything to 10.9.9.9:4317; asked for op-central (147.102.13.108:4317)')
  // Without what the page knows of the operator the in-cluster name is all there is to say, as before.
  assert.equal(compareIntent(central, both, config({ exportEndpoint: '10.9.9.9:4317' }))[0].text, 'Sends everything to 10.9.9.9:4317; asked for op-central (op-central.ikhnos-system.svc:4317)')
  // A sender's own address wins over the operator's: that is what the command dials.
  const own = intent({ destination: { kind: 'operator', endpoint: '', targetOperatorId: 'op-central', address: '10.0.0.5:4317' } as never })
  assert.equal(compareIntent(own, both, config({ exportEndpoint: '10.9.9.9:4317' }), known)[0].text, 'Sends everything to 10.9.9.9:4317; asked for op-central (10.0.0.5:4317)')
  // Another operator's address is not this operator's.
  assert.equal(compareIntent(central, both, config({ exportEndpoint: '10.1.1.1:4317' }), [{ id: 'op-eu', address: '10.1.1.1:4317' }, ...known]).length, 1)
})

test('compareIntent: a destination per signal type is compared signal type by signal type, for the types that run', () => {
  const split = intent({
    signals: [{ id: 'resourceUsage', source: 'builtin' }, { id: 'systemLogs', source: 'builtin' }, { id: 'traces', source: 'builtin' }] as never,
    destination: { kind: 'external', endpoint: 'm:4317' },
    routes: { metrics: { kind: 'external', endpoint: 'm:4317' }, logs: { kind: 'external', endpoint: 'l:4317' }, traces: { kind: 'external', endpoint: 't:9411' } } as never,
  })
  const all = ['resourceUsage', 'systemLogs', 'traces']
  assert.deepEqual(compareIntent(split, all, config({ exportEndpoint: 'traces=t:9411,logs=l:4317,default=m:4317' })), [])
  const wrong = compareIntent(split, all, config({ exportEndpoint: 'traces=z:9411,default=m:4317' }))
  assert.deepEqual(wrong.map((x) => x.text), ['Sends logs to m:4317; asked for l:4317', 'Sends traces to z:9411; asked for t:9411'])
  // One destination installed where each signal type was to go to its own.
  const one = compareIntent(split, all, config({ exportEndpoint: 'm:4317' }))
  assert.deepEqual(one.map((x) => x.text), ['Sends logs to m:4317; asked for l:4317', 'Sends traces to m:4317; asked for t:9411'])
  // Signal types that are not running are not compared: only metrics run, and they go where they were asked.
  assert.deepEqual(compareIntent(intent({ ...split, signals: [{ id: 'resourceUsage', source: 'builtin' }] as never }), ['resourceUsage'], config({ exportEndpoint: 'm:4317' })), [])
})

test('compareIntent: energy and accelerators say which exporter they read when it is not the one asked for', () => {
  const asked = intent({ signals: [{ id: 'resourceUsage', source: 'builtin' }, { id: 'energy', source: 'bundle-kepler' }] as never })
  const d = compareIntent(asked, ['resourceUsage', 'energy'], config({ energySource: 'existing' }))
  assert.deepEqual(d.map((x) => [x.part, x.text]), [['source', 'Energy reads an existing one; asked for the bundled exporter']])
  assert.deepEqual(compareIntent(asked, ['resourceUsage', 'energy'], config({ energySource: 'bundle-kepler' })), [])
  // An agent that does not say which source it reads is not contradicted.
  assert.deepEqual(compareIntent(asked, ['resourceUsage', 'energy'], config()), [])
})

test('compareIntent: the scope goes through the rule that recorded it, so what the wizard wrote agrees with what it installed', () => {
  const applications = ['applicationMetrics', 'traces']
  const signals = applications.map((id) => ({ id, source: 'builtin' }))
  const scopes = { applicationMetrics: { namespaces: ['shop', 'billing'], exclude: ['shop-canary'] }, traces: { namespaces: ['shop'], exclude: ['shop-canary', 'x'] } }
  // What the wizard would record for a draft with these two scopes.
  const draft = { ...emptyTelemetry, applicationMetrics: true, traces: true, applicationMetricsScope: { ...scopes.applicationMetrics, workloads: [] }, tracesScope: { ...scopes.traces, workloads: [] } }
  const recorded = intentScope(draft)
  const agree = intent({ signals: signals as never, ...recorded })
  assert.deepEqual(compareIntent(agree, applications, config({ scope: scopes })), [])
  const moved = compareIntent(agree, applications, config({ scope: { ...scopes, traces: { namespaces: ['other'], exclude: [] } } }))
  assert.equal(moved[0].part, 'scope')
  assert.equal(moved[0].text, 'Collects from billing, other, shop; asked for billing, shop, without shop-canary')
  // An agent that does not report the scope cannot be compared on it.
  assert.deepEqual(compareIntent(agree, applications, config()), [])
  // With a signal more or less the scope would differ for that reason alone: only the signals are said.
  const fewer = compareIntent(agree, ['applicationMetrics'], config({ scope: { applicationMetrics: scopes.applicationMetrics } }))
  assert.deepEqual(fewer.map((x) => x.part), ['signals'])
})
