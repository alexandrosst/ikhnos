import { describe, expect, test } from 'vitest'
import type { AgentDiagnostics, AgentExportHealth } from '@/lib/consent'
import { emptyTelemetry } from '@/lib/install'
import { ARRIVAL_PATIENCE_SECONDS, arrivalCheck, clusterTelemetryLine, reviewNotes } from '@/features/pipeline/lib/telemetrySetup'

const NOW = Date.parse('2026-10-06T12:10:00Z')
const iso = (secondsAgo: number) => new Date(NOW - secondsAgo * 1000).toISOString()

function diag(o: { installed?: string[]; health?: AgentExportHealth; reportedAgo?: number } = {}): AgentDiagnostics {
  return { reportedAt: iso(o.reportedAgo ?? 30), agentVersion: '1', uptimeSeconds: 1, installedTier: 2, approvedTier: 2, effectiveTier: 2, pausedCollectors: [], excludedNamespaces: 0, collectors: [], informers: [], problems: [], installedTelemetry: o.installed ?? ['resourceUsage'], exportHealth: o.health }
}
const route = (signal: string, state: 'waiting' | 'exporting' | 'silent' | 'failing', more: object = {}) => ({ exporter: 'otlp', signal, state, sent: 5, failed: 0, ...more })
const check = (d: AgentDiagnostics | undefined, o: { wanted?: string[]; waited?: number; connected?: boolean; namespace?: string } = {}) =>
  arrivalCheck({ wanted: o.wanted ?? ['resourceUsage'], diagnostics: d, connected: o.connected, waited: o.waited ?? 0, now: NOW, cluster: 'vradipus', namespace: o.namespace })
const received = (signal: string, state: 'waiting' | 'receiving' | 'silent' | 'refusing', more: object = {}) => ({ signal, state, accepted: 0, refused: 0, ...more })

describe('arrivalCheck: whether the data that was just set up arrives', () => {
  test('no report from the agent at all is Unknown, with the one thing to do', () => {
    const a = check(undefined)
    expect(a.health).toBe('unknown')
    expect(a.todo).toMatch(/discovery agent of vradipus is running/)
  })

  test('an agent that has gone quiet is Unknown, not whatever it last said; so is one that is not connected', () => {
    const h: AgentExportHealth = { podsReached: 1, podsFailed: 0, routes: [route('metrics', 'exporting', { lastSentAt: iso(40) })] }
    expect(check(diag({ health: h, reportedAgo: 3600 })).headline).toMatch(/has not reported for 60 min/)
    expect(check(diag({ health: h }), { connected: false }).health).toBe('unknown')
  })

  test('a signal the agent does not yet report running is waiting for the command; after the patience it says the command was probably not applied', () => {
    const d = diag({ installed: [], health: { podsReached: 1, podsFailed: 0, routes: [] } })
    const early = check(d, { waited: 60 })
    expect(early.health).toBe('unknown')
    expect(early.headline).toMatch(/Waiting for the command to take effect.*Resource usage/)
    expect(early.todo).toBeUndefined()
    const late = check(d, { waited: ARRIVAL_PATIENCE_SECONDS + 1 })
    expect(late.health).toBe('attention')
    expect(late.todo).toMatch(/Run the command in vradipus/)
  })

  test('a lane whose data waits in the collector\'s queue says so, and does not claim sends that were never counted as failed', () => {
    const h: AgentExportHealth = { podsReached: 3, podsFailed: 0, routes: [route('logs', 'failing', { sent: 0, failed: 0, lastFailedAt: iso(30) })] }
    const a = check(diag({ installed: ['systemLogs'], health: h }), { wanted: ['systemLogs'] })
    expect(a.rows[0].health).toBe('down')
    expect(a.rows[0].sentence).toMatch(/waiting to be sent.*keeps trying.*none has got through/)
    expect(a.rows[0].sentence).not.toMatch(/0 sends failed/)
  })

  test('a lane whose data waits says how much, and what the queue refused', () => {
    const h: AgentExportHealth = { podsReached: 3, podsFailed: 0, routes: [route('logs', 'failing', { sent: 0, failed: 0, lastFailedAt: iso(30), queued: 90, queueCapacity: 100, dropped: 12, lastDroppedAt: iso(60) })] }
    const a = check(diag({ installed: ['systemLogs'], health: h }), { wanted: ['systemLogs'] })
    expect(a.rows[0].health).toBe('down')
    expect(a.rows[0].sentence).toMatch(/90 items are queued of the 100 the queue holds/)
    expect(a.rows[0].sentence).toMatch(/12 items were dropped because the queue was full \(the last 60 s ago\)/)
  })

  test('data that goes out but is dropped, or soon will be, is not green: the destination is too slow for the collector', () => {
    const lane = (more: object) => check(diag({ installed: ['systemLogs'], health: { podsReached: 3, podsFailed: 0, routes: [route('logs', 'exporting', { lastSentAt: iso(10), ...more })] } }), { wanted: ['systemLogs'] })

    const dropping = lane({ queued: 100, queueCapacity: 100, dropped: 12, lastDroppedAt: iso(120) })
    expect(dropping.rows[0].health).toBe('attention')
    expect(dropping.rows[0].sentence).toMatch(/takes it slower than the collector sends it: 12 items were dropped/)
    expect(dropping.health).toBe('attention')
    expect(dropping.headline).toMatch(/some of it is being dropped/)
    expect(dropping.todo).toMatch(/Look at its load, or collect less/)

    const nearly = lane({ queued: 85, queueCapacity: 100 })
    expect(nearly.rows[0].health).toBe('attention')
    expect(nearly.rows[0].sentence).toMatch(/queue is 85 % full \(85 of 100\)/)

    // A queue that is mostly empty, with no capacity said, and a drop long ago are not reasons to look.
    expect(lane({ queued: 10, queueCapacity: 100 }).rows[0].health).toBe('healthy')
    expect(lane({ queued: 99 }).rows[0].health).toBe('healthy')
    const past = lane({ queued: 0, queueCapacity: 100, dropped: 3, lastDroppedAt: iso(2 * 3600) })
    expect(past.rows[0].health).toBe('healthy')
    expect(past.rows[0].sentence).toMatch(/3 items were dropped.*None lately/)
    expect(past.headline).toBe('Data is arriving.')
  })

  test('an agent that does not report export health, or cannot read the counters, is said so with what to look at', () => {
    expect(check(diag()).headline).toMatch(/does not report whether data leaves the cluster/)
    const bad = check(diag({ health: { podsReached: 0, podsFailed: 2, lastError: 'no such host', routes: [] } }))
    expect(bad.health).toBe('attention')
    expect(bad.headline).toMatch(/cannot read the collectors.*no such host/)
    expect(bad.todo).toMatch(/network policy/)
  })

  test('each signal type is Healthy with its last data, Not working when sends fail, and a quiet one is not an error', () => {
    const h: AgentExportHealth = {
      podsReached: 3,
      podsFailed: 0,
      routes: [route('metrics', 'exporting', { lastSentAt: iso(120) }), route('logs', 'failing', { sent: 0, failed: 12, lastFailedAt: iso(30) })],
    }
    const a = check(diag({ installed: ['resourceUsage', 'systemLogs'], health: h }), { wanted: ['resourceUsage', 'systemLogs'] })
    const [metrics, logs] = a.rows
    expect(metrics).toMatchObject({ modality: 'metrics', health: 'healthy', lastData: 'Last data 2 min ago' })
    expect(logs).toMatchObject({ modality: 'logs', health: 'down', lastData: 'No data yet' })
    expect(logs.sentence).toMatch(/12 sends failed .*none got through/)
    expect(a.health).toBe('down')
    expect(a.todo).toMatch(/Where to send/)

    const quiet = check(diag({ health: { podsReached: 1, podsFailed: 0, routes: [route('metrics', 'silent', { lastSentAt: iso(3000) })] } }))
    expect(quiet.health).toBe('healthy')
    expect(quiet.rows[0].sentence).toMatch(/normal when nothing is producing metrics/)
  })

  test('nothing gone out yet is Unknown at first and Needs attention once the patience has run out', () => {
    const d = diag({ health: { podsReached: 1, podsFailed: 0, routes: [] } })
    expect(check(d, { waited: 30 })).toMatchObject({ health: 'unknown', headline: 'Waiting for the first data.' })
    const late = check(d, { waited: ARRIVAL_PATIENCE_SECONDS })
    expect(late.health).toBe('attention')
    expect(late.rows[0].lastData).toBe('No data yet')
    expect(late.todo).toMatch(/collector pods in vradipus/)
  })

  test('collector reads that failed are named, and the rest still count', () => {
    const h: AgentExportHealth = { podsReached: 2, podsFailed: 1, lastError: 'timeout', routes: [route('metrics', 'exporting', { lastSentAt: iso(20) })] }
    expect(check(diag({ health: h })).partial).toBe('1 of 3 collector reads failed on the last attempt (timeout); the rest still count.')
  })
})

describe('reviewNotes: the review in plain words', () => {
  const draft = { ...emptyTelemetry, resourceUsage: true, traces: true, tracesSamplingPercent: 20, redaction: true, tags: [{ key: 'team', value: 'sre' }] }

  test('before anything is installed it says what starts, where it goes, and what it adds', () => {
    const n = reviewNotes({ draft, diff: [], installed: false, turningOff: false, destination: 'eu-west', kept: [] })
    expect(n.changes).toContain('Sends it to eu-west.')
    expect(n.changes.join(' ')).toMatch(/Starts collecting .*Resource usage.*Traces/)
    expect(n.changes).toContain('Masks values that look like secrets before anything leaves the cluster.')
    expect(n.changes).toContain('Keeps 20% of traces.')
    expect(n.changes).toContain('Adds one tag to everything it sends.')
  })

  test('with one destination per signal type it says where each goes instead of one place', () => {
    const n = reviewNotes({ draft, diff: [], installed: false, turningOff: false, destination: '2 destinations', kept: [], routes: ['metrics to Mimir', 'traces to Tempo'] })
    expect(n.changes).toContain('Sends metrics to Mimir and traces to Tempo.')
    expect(n.changes.join(' ')).not.toContain('Sends it to')
  })

  test('with an install it is the diff against it, or says nothing changes', () => {
    expect(reviewNotes({ draft, diff: ['Turns on Traces.'], installed: true, turningOff: false, destination: 'x', kept: [] }).changes).toEqual(['Turns on Traces.'])
    expect(reviewNotes({ draft, diff: [], installed: true, turningOff: false, destination: 'x', kept: [] }).changes[0]).toMatch(/^Nothing: this is already how the install is set up/)
  })

  test('what does not change names access and applications, and what is kept as installed', () => {
    const n = reviewNotes({ draft, diff: [], installed: true, turningOff: false, destination: 'x', kept: ['the protocol', 'the credential'] })
    expect(n.unchanged[0]).toMatch(/access level stays as approved/)
    expect(n.unchanged[1]).toMatch(/nothing in them is edited/)
    expect(n.unchanged[2]).toBe("Everything else in the agent's release, including the protocol and the credential.")
  })

  test('how to stop is to run the setup with nothing picked, and what stays at the destination', () => {
    const n = reviewNotes({ draft, diff: [], installed: false, turningOff: false, destination: 'x', kept: [] })
    expect(n.stop[0]).toMatch(/nothing picked: the command turns every signal off/)
    expect(n.stop[1]).toMatch(/already delivered stays at its destination/)
  })

  test('turning everything off says exactly that, and how to turn it back on', () => {
    const n = reviewNotes({ draft: emptyTelemetry, diff: [], installed: true, turningOff: true, destination: '', kept: [] })
    expect(n.changes[0]).toMatch(/Turns every telemetry signal off/)
    expect(n.stop[0]).toMatch(/pick signals to turn them back on/)
  })
})

describe('clusterTelemetryLine: what a cluster does today, for the list that picks one', () => {
  test('nothing set up, or what it sends and when the last data went out', () => {
    expect(clusterTelemetryLine(undefined, NOW)).toBe('No telemetry set up yet')
    expect(clusterTelemetryLine(diag({ installed: [] }), NOW)).toBe('No telemetry set up yet')
    expect(clusterTelemetryLine(diag({ installed: ['resourceUsage', 'systemLogs', 'traces'], health: { podsReached: 1, podsFailed: 0, routes: [route('metrics', 'exporting', { lastSentAt: iso(120) })] } }), NOW)).toBe('Sends metrics, logs and traces · Last data 2 min ago')
    expect(clusterTelemetryLine(diag({ installed: ['resourceUsage'] }), NOW)).toBe('Sends metrics · No data yet')
  })

  describe('the stage that stopped the data, for what applications push', () => {
    const traces = (receivers: object[], waited: number, routes: object[] = []) =>
      check(diag({ installed: ['traces'], health: { podsReached: 1, podsFailed: 0, routes: routes as never, receivers: receivers as never } }), { wanted: ['traces'], waited, namespace: 'ikhnos' })

    test('traces that never reached the collector say so, and name the address to send to', () => {
      const early = traces([], 30)
      expect(early.health).toBe('unknown')
      expect(early.rows[0].sentence).toMatch(/Nothing has reached the collector/)
      expect(early.otlp).toEqual({ grpc: 'ikhnos-telemetry-cluster.ikhnos.svc:4317', http: 'ikhnos-telemetry-cluster.ikhnos.svc:4318' })
      const late = traces([], ARRIVAL_PATIENCE_SECONDS)
      expect(late.health).toBe('attention')
      expect(late.headline).toBe('No application has sent anything to the collector.')
      expect(late.todo).toMatch(/OTLP exporter of the application/)
    })

    test('traces the collector took in but did not send on name the stage after it', () => {
      const a = traces([received('traces', 'receiving', { accepted: 40 })], ARRIVAL_PATIENCE_SECONDS)
      expect(a.rows[0].sentence).toMatch(/reached the collector \(40 received\) but none has gone out.*between the collector and the destination/)
      expect(a.otlp).toBeUndefined()
      expect(a.todo).toMatch(/collector pods in vradipus/)
    })

    test('a receiver that turned data away is not blamed on the destination', () => {
      const a = traces([received('traces', 'refusing', { accepted: 2, refused: 9, lastRefusedAt: iso(20) })], 0)
      expect(a.health).toBe('down')
      expect(a.headline).toBe('The collector is turning away data from your applications.')
      expect(a.rows[0].sentence).toMatch(/turned away 9 traces.*20 s ago/)
    })

    test('traces that went out are healthy, and an exporter that failed still blames the destination', () => {
      expect(traces([received('traces', 'receiving', { accepted: 5 })], 0, [route('traces', 'exporting', { lastSentAt: iso(10) })]).health).toBe('healthy')
      const failing = traces([received('traces', 'receiving', { accepted: 5 })], 0, [route('traces', 'failing', { sent: 0, failed: 3, lastFailedAt: iso(10) })])
      expect(failing.headline).toMatch(/destination is refusing/)
    })

    test('infrastructure that only the agent reads is never told to wait for an application', () => {
      const a = check(diag({ health: { podsReached: 1, podsFailed: 0, routes: [], receivers: [] } }), { waited: ARRIVAL_PATIENCE_SECONDS })
      expect(a.otlp).toBeUndefined()
      expect(a.rows[0].sentence).toMatch(/Nothing has gone out after/)
    })

    test('an agent that reports no receivers at all (an older one) is read as nothing seen', () => {
      const a = check(diag({ installed: ['traces'], health: { podsReached: 1, podsFailed: 0, routes: [] } as never }), { wanted: ['traces'], waited: 0 })
      expect(a.rows[0].sentence).toMatch(/Nothing has reached the collector/)
    })
  })
})
