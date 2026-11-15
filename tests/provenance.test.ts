// Observation state and evidence: the pure rules behind the chips. The server computes the states; these pin how the browser reads and words them.
import assert from 'node:assert/strict'
import { test } from 'node:test'
import { ageLabel, CONFIDENCE_TONE, dependencyProvenance, evidenceLevel, evidenceRows, formatAttr, goneInfo, indexModel, needsEvidenceChip, observation, sourceLabel, targetStatus, weakAttributes, type ModelEntity } from '../src/lib/provenance'
import { DEFAULT_ORG, type Cluster, type Dependency, type MachineNode, type Service, type Site } from '../src/lib/types'

const NOW = Date.parse('2026-09-21T12:00:00Z')
const iso = (msAgo: number) => new Date(NOW - msAgo).toISOString()

test('ageLabel words an age the way the server does', () => {
  assert.equal(ageLabel(iso(45_000), NOW), '45 s')
  assert.equal(ageLabel(iso(12 * 60_000), NOW), '12 min')
  assert.equal(ageLabel(iso(2 * 3600_000), NOW), '2 h')
  assert.equal(ageLabel(iso(3 * 86400_000), NOW), '3 d')
  assert.equal(ageLabel(undefined, NOW), '')
  assert.equal(ageLabel('not a date', NOW), '')
})

test('observation: hand-typed records have no state; discovered ones do, and only live is actionable', () => {
  assert.equal(observation({ source: 'manual' }), undefined)
  assert.equal(observation(undefined), undefined)
  const live = observation({ source: 'discovered', state: 'live' })!
  assert.deepEqual([live.kind, live.label, live.actionable], ['live', 'live', true])
  const stale = observation({ source: 'discovered', state: 'stale', stateReason: 'stale for 2 h' })!
  assert.deepEqual([stale.kind, stale.label, stale.actionable, stale.reason], ['stale', 'stale 2 h', false, 'stale for 2 h'])
  const off = observation({ source: 'discovered', state: 'disconnected', stateReason: 'agent not connected' })!
  assert.deepEqual([off.label, off.actionable], ['disconnected', false])
  const revoked = observation({ source: 'discovered', state: 'revoked', stateReason: 'agent revoked 3 h ago' })!
  assert.deepEqual([revoked.label, revoked.actionable], ['revoked', false])
  // an older server that only sends the stale flag is still understood
  assert.equal(observation({ source: 'discovered', stale: true })!.kind, 'stale')
  assert.equal(observation({ source: 'discovered' })!.kind, 'live')
})

test('a record with deletedAt, or a tombstone, reads as gone', () => {
  const g = observation({ source: 'discovered', state: 'live', deletedAt: iso(1000) })!
  assert.deepEqual([g.kind, g.label, g.actionable], ['gone', 'gone', false])
  const t = goneInfo({ goneAt: iso(3 * 3600_000), reason: 'no longer reported by its agent' }, NOW)
  assert.equal(t.label, 'gone')
  assert.match(t.reason ?? '', /3 h ago/)
})

test('targetStatus: says why a cluster is not a target, and unknown capacity is not room', () => {
  assert.deepEqual(targetStatus({ source: 'manual' }, false), { eligible: true, state: 'declared' })
  assert.equal(targetStatus({ source: 'discovered', state: 'live' }, true).eligible, true)
  const s = targetStatus({ source: 'discovered', state: 'stale', stateReason: 'stale for 2 h' }, true)
  assert.deepEqual([s.eligible, s.reason], [false, 'stale for 2 h'])
  const r = targetStatus({ source: 'discovered', state: 'revoked', stateReason: 'agent revoked' }, true)
  assert.deepEqual([r.eligible, r.reason], [false, 'agent revoked'])
  const c = targetStatus({ source: 'discovered', state: 'live' }, false)
  assert.equal(c.eligible, false)
  assert.match(c.reason ?? '', /capacity unknown/)
})

test('evidence chips appear for guesses and unknowns, not for the normal case', () => {
  assert.equal(needsEvidenceChip('guess'), true)
  assert.equal(needsEvidenceChip('unknown'), true)
  assert.equal(needsEvidenceChip('reported'), false)
  assert.equal(needsEvidenceChip('measured'), false)
  assert.equal(evidenceLevel({ confidence: 'low' }), 'guess')
  assert.equal(evidenceLevel({ confidence: 'high' }, true), 'measured')
  assert.equal(evidenceLevel({ confidence: 'high' }), 'inferred')
})

const base = { orgId: DEFAULT_ORG, source: 'discovered' as const }
test('weakAttributes: guessed and unknown values of a discovered record; a person\'s override is not weak', () => {
  const rec = { ...base, id: 'n1', region: '', version: 'v1', evidence: { provider: { signal: 'node names look like EKS', confidence: 'low' } }, provider: 'aws' } as never
  const w = weakAttributes('cluster', rec)
  assert.deepEqual(w.map((x) => [x.field, x.level]), [['provider', 'guess'], ['region', 'unknown']])
  assert.match(w[0].why, /EKS/)
  const overridden = { ...(rec as object), overrides: { region: 'eu-west-1', provider: 'gcp' } } as never
  assert.deepEqual(weakAttributes('cluster', overridden), [])
  assert.deepEqual(weakAttributes('cluster', { ...(rec as object), source: 'manual' } as never), [])
  const svc = weakAttributes('service', { ...base, id: 's', cpuRequestM: undefined, memRequestMi: 128 } as never)
  assert.deepEqual(svc.map((x) => x.field), ['cpuRequestM'])
})

test('formatAttr: units are explicit and unknown is never a zero', () => {
  assert.equal(formatAttr({ value: 4, unit: 'cores', source: 'agent', confidence: 'reported', state: 'live' }), '4 cores')
  assert.equal(formatAttr({ value: 3 * 1024 ** 3, unit: 'bytes', source: 'agent', confidence: 'reported', state: 'live' }), '3 GiB')
  assert.equal(formatAttr({ value: 3, unit: 'count', source: 'agent', confidence: 'reported', state: 'live' }), '3')
  assert.equal(formatAttr({ value: null, source: 'agent', confidence: 'unknown', state: 'live' }), 'unknown')
  assert.equal(formatAttr({ value: 0, source: 'agent', confidence: 'unknown', state: 'live' }), 'unknown')
  assert.equal(formatAttr({ value: 0, unit: 'cores', source: 'agent', confidence: 'reported', state: 'live' }), '0 cores')
  assert.equal(formatAttr({ value: true, source: 'declared', confidence: 'reported', state: 'live' }), 'yes')
})

test('evidenceRows: weakest first, with source and time, shadowed observed values kept', () => {
  const e: ModelEntity = {
    kind: 'node', id: 'nd-1', name: 'w1', origin: 'observed+declared', state: 'live',
    attributes: {
      cpuAllocatable: { value: 8, unit: 'cores', source: 'agent', agentId: 'ag-1', confidence: 'reported', observedAt: '2026-09-21T11:59:00Z', state: 'live' },
      region: { value: null, source: 'agent', confidence: 'unknown', state: 'live', evidence: 'no node carries a region label' },
      provider: { value: 'aws', source: 'inferred', confidence: 'guess', state: 'live', evidence: 'node names' },
      zone: { value: 'a', source: 'declared', confidence: 'reported', state: 'live', shadowed: { value: 'b', source: 'agent', confidence: 'reported', state: 'live' } },
    },
  }
  const rows = evidenceRows(e, (id) => (id === 'ag-1' ? 'edge-agent' : undefined))
  assert.deepEqual(rows.map((r) => r.attribute), ['region', 'provider', 'cpuAllocatable', 'zone'])
  assert.equal(rows[0].value, 'unknown')
  assert.equal(rows[2].source, 'agent (edge-agent)')
  assert.equal(rows[2].unit, 'cores')
  assert.equal(rows[3].shadowed, 'b')
  assert.equal(sourceLabel({ source: 'declared' }), 'declared by a person')
  assert.deepEqual([...indexModel({ entities: [e] } as never).keys()], ['node|nd-1'])
  assert.deepEqual(evidenceRows(undefined), [])
})

test('CONFIDENCE_TONE: high/medium/low map onto the app\'s own ok/warn/bad palette, same as everywhere else', () => {
  assert.deepEqual(CONFIDENCE_TONE, { high: 'ok', medium: 'warn', low: 'bad' })
})

test('dependencyProvenance: sources joined, and the measurement method only when it is known', () => {
  assert.deepEqual(dependencyProvenance({ sources: ['observed'], via: 'ebpf' }), { source: 'observed', via: 'eBPF' })
  assert.deepEqual(dependencyProvenance({ sources: ['declared', 'observed'], via: 'conntrack' }), { source: 'declared + observed', via: 'conntrack' })
  assert.deepEqual(dependencyProvenance({ sources: ['declared'] }), { source: 'declared', via: undefined })
})
