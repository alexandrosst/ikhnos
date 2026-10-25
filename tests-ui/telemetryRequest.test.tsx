import { describe, expect, test } from 'vitest'
import type { TelemetryRequestDoc } from '@/lib/generated/wire'
import { emptyTelemetry } from '@/lib/install'
import { askAvailability, isSignalChange, newestRequest, notInARequest, requestChange, requestLine, requestOf, sameAsRequest, shareProblem, widens } from '@/lib/telemetryRequest'

const doc = (over: Partial<TelemetryRequestDoc> = {}): TelemetryRequestDoc => ({ revision: 1, requested: true, state: 'waiting', ...over })
const sigs = (on: string[]) => Object.fromEntries(['resourceUsage', 'energy', 'kubernetesState', 'nodeRuntime', 'networkLatency', 'clusterLinks', 'applicationMetrics', 'systemLogs', 'kubernetesEvents', 'applicationLogs', 'traces', 'accelerators'].map((k) => [k, on.includes(k)])) as unknown as TelemetryRequestDoc['signals']

describe('askAvailability', () => {
  test('needs a controller that keeps the agent, and an agent that can pass a request on', () => {
    expect(askAvailability(undefined, ['telemetry-relay'])).toBe('none')
    expect(askAvailability(false, ['telemetry-relay'])).toBe('none')
    expect(askAvailability(true, [])).toBe('old')
    expect(askAvailability(true, undefined)).toBe('old')
    expect(askAvailability(true, ['telemetry-relay'])).toBe('yes')
  })
})

describe('requestOf', () => {
  test('states all twelve signals, and the share only while traces are on', () => {
    const r = requestOf({ ...emptyTelemetry, resourceUsage: true })
    expect(Object.keys(r.signals)).toHaveLength(12)
    expect(r.tracesSamplingPercent).toBeUndefined()
    const t = requestOf({ ...emptyTelemetry, traces: true, tracesSamplingPercent: 25 })
    expect(t.signals.traces).toBe(true)
    expect(t.tracesSamplingPercent).toBe(25)
  })
  test('has nothing about where the data goes', () => {
    expect(Object.keys(requestOf({ ...emptyTelemetry, exportEndpoint: 'x:4317', traces: true }))).toEqual(['signals', 'tracesSamplingPercent'])
  })
})

describe('shareProblem', () => {
  test('is a whole number from 1 to 100, and only matters with traces on', () => {
    expect(shareProblem({ ...emptyTelemetry, traces: false, tracesSamplingPercent: 0 })).toBeUndefined()
    expect(shareProblem({ ...emptyTelemetry, traces: true, tracesSamplingPercent: 0 })).toMatch(/1 to 100/)
    expect(shareProblem({ ...emptyTelemetry, traces: true, tracesSamplingPercent: 12.5 })).toMatch(/1 to 100/)
    expect(shareProblem({ ...emptyTelemetry, traces: true, tracesSamplingPercent: 100 })).toBeUndefined()
  })
})

describe('requestChange', () => {
  test('is what turns on, what turns off, and whether the share moves', () => {
    const d = { ...emptyTelemetry, resourceUsage: true, traces: true, tracesSamplingPercent: 25 }
    expect(requestChange(['resourceUsage', 'kubernetesEvents'], undefined, d)).toEqual({ on: ['traces'], off: ['kubernetesEvents'], share: true })
    expect(requestChange(['resourceUsage', 'traces'], 25, d)).toEqual({ on: [], off: [], share: false })
  })
})

describe('what a request leaves out', () => {
  test('only signals turned on or off are carried', () => {
    expect(isSignalChange('Turns on Traces')).toBe(true)
    expect(isSignalChange('Sends to a:1 instead of b:2')).toBe(false)
    expect(notInARequest(['Turns on Traces', 'Sends to a:1 instead of b:2', 'Changes the tags stamped on everything it sends'])).toEqual(['Sends to a:1 instead of b:2', 'Changes the tags stamped on everything it sends'])
  })
  test('where a signal is collected from is said for what it means here: it stays as the owner set it', () => {
    expect(notInARequest(['Turns on Traces', 'Collects traces from all namespaces'])).toEqual(['Where traces are collected from stays as the owner set it'])
    expect(notInARequest(['Changes where application logs are collected from all namespaces to payments'])).toEqual(['Where application logs are collected from stays as the owner set it'])
  })
})

describe('sameAsRequest', () => {
  test('is true only for the request in force', () => {
    const d = { ...emptyTelemetry, resourceUsage: true }
    expect(sameAsRequest(doc({ signals: sigs(['resourceUsage']) }), d)).toBe(true)
    expect(sameAsRequest(doc({ signals: sigs(['resourceUsage', 'traces']) }), d)).toBe(false)
    expect(sameAsRequest(doc({ signals: sigs(['resourceUsage']), requested: false }), d)).toBe(false)
    expect(sameAsRequest(undefined, d)).toBe(false)
  })
})

describe('newestRequest', () => {
  test('the higher revision wins, and the polled one on a tie', () => {
    const a = doc({ revision: 3, state: 'applied' })
    const b = doc({ revision: 4, state: 'waiting' })
    expect(newestRequest(a, b)).toBe(b)
    expect(newestRequest(b, a)).toBe(b)
    const same = doc({ revision: 4, state: 'applied' })
    expect(newestRequest(same, b)).toBe(same)
    expect(newestRequest(undefined, b)).toBe(b)
    expect(newestRequest(a, undefined)).toBe(a)
  })
})

describe('widens (the server’s rule for who may ask)', () => {
  test('turning anything on, or keeping more traces, widens; asking for less does not', () => {
    expect(widens(undefined, { signals: sigs(['resourceUsage']) })).toBe(true)
    expect(widens(doc({ signals: sigs(['resourceUsage', 'traces']) }), { signals: sigs(['resourceUsage']) })).toBe(false)
    expect(widens(doc({ signals: sigs(['traces']), tracesSamplingPercent: 25 }), { signals: sigs(['traces']), tracesSamplingPercent: 50 })).toBe(true)
    expect(widens(doc({ signals: sigs(['traces']), tracesSamplingPercent: 50 }), { signals: sigs(['traces']), tracesSamplingPercent: 25 })).toBe(false)
    // A request taken back is the cluster's own settings again: the next one is judged from nothing.
    expect(widens(doc({ requested: false }), { signals: sigs(['resourceUsage']) })).toBe(true)
    expect(widens(undefined, { signals: sigs([]) })).toBe(false)
  })
})

describe('requestLine', () => {
  test('has nothing to say about a cluster that was never asked', () => {
    expect(requestLine(undefined)).toBeNull()
    expect(requestLine(doc({ revision: 0 }))).toBeNull()
  })
  test('uses the cluster’s own reason, and says so when it gave none', () => {
    expect(requestLine(doc({ state: 'refused', reason: 'because' }))?.body).toBe('because')
    expect(requestLine(doc({ state: 'refused' }))?.body).toMatch(/no reason/)
    expect(requestLine(doc({ state: 'reverted' }))?.tone).toBe('warn')
  })
})
