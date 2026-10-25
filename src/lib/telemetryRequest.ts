import type * as W from './generated/wire'
import { PICKABLE_SIGNALS, TELEMETRY_SIGNALS, type TelemetryInput } from './install'

/**
 * Asking a cluster to collect more or less. The server only keeps the request and hands it to the agent; the cluster's controller applies it
 * inside what its owner allowed, or says why not. It is the "what" - which signals, and how much of the traces - and never the "where":
 * the destination, its credentials, the scopes and the tags stay with whoever runs the cluster, so none of them is in a request.
 */

/** What an agent says it does when it can pass a request to its controller (internal/agentcaps). */
export const RELAY_CAPABILITY = 'telemetry-relay'

/**
 * Whether this page can offer to ask the cluster. `yes`: a controller keeps the agent and the agent can pass a request on. `old`: a
 * controller keeps it, but the agent is from before the relay. `none`: nothing keeps this agent installed (or the server has not heard from
 * it since it started), so there is nobody in the cluster to take a request.
 */
export type AskAvailability = 'yes' | 'old' | 'none'

export function askAvailability(kept: boolean | undefined, capabilities: readonly string[] | undefined): AskAvailability {
  if (!kept) return 'none'
  return (capabilities ?? []).includes(RELAY_CAPABILITY) ? 'yes' : 'old'
}

/** The request a draft makes: every signal stated (a signal not true is off), and the share of traces kept while traces are on. */
export function requestOf(draft: TelemetryInput): { signals: W.TelemetrySignals; tracesSamplingPercent?: number } {
  const rec = draft as unknown as Record<string, boolean>
  const signals = Object.fromEntries(TELEMETRY_SIGNALS.map((s) => [s.id, rec[s.id] === true])) as unknown as W.TelemetrySignals
  return signals.traces ? { signals, tracesSamplingPercent: draft.tracesSamplingPercent } : { signals }
}

/** The lines of a form's review that a request does carry: signals turned on or off. Every other line is a change a request leaves out. */
export const isSignalChange = (line: string): boolean => /^Turns (on|off) /.test(line)

/**
 * The changes in a form's review that a request cannot carry (destination, tags, scopes, processors...): they need the command. A line
 * about where a signal is collected from ("Collects traces from all namespaces") is what the form would state for a signal turned on, not
 * something to be done by the request, so it is said for what it means here: the scope stays as the owner set it.
 */
export function notInARequest(changes: readonly string[]): string[] {
  const out: string[] = []
  for (const l of changes) {
    if (isSignalChange(l)) continue
    const m = /^(?:Collects (.+) from .+|Changes where (.+) are collected from .+)$/.exec(l)
    const line = m ? `Where ${m[1] ?? m[2]} are collected from stays as the owner set it` : l
    if (!out.includes(line)) out.push(line)
  }
  return out
}

/** What the share of traces is asked to be, or why it cannot be: the server takes a whole number from 1 to 100, and only with traces on. */
export function shareProblem(draft: TelemetryInput): string | undefined {
  if (!draft.traces) return undefined
  const p = draft.tracesSamplingPercent
  return Number.isInteger(p) && p >= 1 && p <= 100 ? undefined : 'Traces: the share to keep is a whole number from 1 to 100.'
}

/** The signals a request turns on or off, against what the cluster collects now, and whether the share of traces changes. */
export function requestChange(installed: readonly string[], installedShare: number | undefined, draft: TelemetryInput): { on: string[]; off: string[]; share: boolean } {
  const rec = draft as unknown as Record<string, boolean>
  const wanted = PICKABLE_SIGNALS.filter((s) => rec[s.id]).map((s) => s.id)
  const on = wanted.filter((id) => !installed.includes(id))
  const off = installed.filter((id) => !wanted.includes(id) && PICKABLE_SIGNALS.some((s) => s.id === id))
  const share = !!draft.traces && draft.tracesSamplingPercent !== (installedShare ?? 100)
  return { on, off, share }
}

/** Whether a request already asks for exactly what a draft would (so sending it again changes nothing, and the server would answer with the same revision). */
export function sameAsRequest(doc: W.TelemetryRequestDoc | undefined, draft: TelemetryInput): boolean {
  if (!doc?.requested || !doc.signals) return false
  const want = requestOf(draft)
  const a = doc.signals as unknown as Record<string, boolean>
  const b = want.signals as unknown as Record<string, boolean>
  if (TELEMETRY_SIGNALS.some((s) => a[s.id] !== b[s.id])) return false
  return (doc.tracesSamplingPercent ?? undefined) === (want.tracesSamplingPercent ?? undefined)
}

/** The newer of two answers about the same agent's request: the one with the higher revision, and on a tie the polled one. */
export function newestRequest(polled: W.TelemetryRequestDoc | undefined, sent: W.TelemetryRequestDoc | undefined): W.TelemetryRequestDoc | undefined {
  if (!sent) return polled
  if (!polled) return sent
  return sent.revision > polled.revision ? sent : polled
}

/** What the server's rule is for who may ask: turning anything on, or keeping more of the traces, takes an administrator. */
export function widens(cur: W.TelemetryRequestDoc | undefined, next: { signals?: W.TelemetrySignals; tracesSamplingPercent?: number }): boolean {
  const was = (cur?.requested ? cur.signals : undefined) as unknown as Record<string, boolean> | undefined
  const now = next.signals as unknown as Record<string, boolean> | undefined
  if (now && TELEMETRY_SIGNALS.some((s) => now[s.id] && !was?.[s.id])) return true
  const share = (t: { tracesSamplingPercent?: number } | undefined) => t?.tracesSamplingPercent ?? 100
  return next.tracesSamplingPercent !== undefined && share(next) > share(cur?.requested ? cur : undefined)
}

export type LineTone = 'info' | 'ok' | 'bad' | 'warn'

export interface RequestLine {
  tone: LineTone
  title: string
  body: string
  meta: string
}

/**
 * What the line under the form says about the latest request, in the cluster's own words where it gave a reason. `waiting` is the server's
 * word that it has heard nothing about this revision yet; `written` is the agent's that it has handed the request to the controller; the
 * rest are the cluster's.
 */
export function requestLine(doc: W.TelemetryRequestDoc | undefined): RequestLine | null {
  if (!doc || doc.revision === 0) return null
  const meta = `Revision ${doc.revision}`
  const back = !doc.requested
  switch (doc.state) {
    case 'written':
      return { tone: 'info', title: back ? 'The controller is taking it back' : 'The controller is applying it', body: back ? 'It puts the collectors back as their owner wrote them and waits for them to be ready.' : 'It changes the telemetry collectors and waits for them to be ready. This takes up to 3 minutes, and if they do not come up the cluster is put back.', meta }
    case 'applied':
      return back
        ? { tone: 'ok', title: 'Taken back', body: 'The cluster is back on the settings its owner wrote.', meta }
        : { tone: 'ok', title: 'In force', body: 'The cluster collects what was asked.', meta }
    case 'refused':
      return { tone: 'bad', title: 'The cluster did not take it', body: doc.reason || 'It gave no reason. Nothing was changed.', meta: `${meta} · change the request to try again` }
    case 'reverted':
      return { tone: 'warn', title: 'Put back', body: doc.reason || 'The change did not come up, so the cluster went back to its own settings.', meta: `${meta} · change the request to try again` }
    default:
      return { tone: 'info', title: back ? 'Taking it back' : 'Sent to the agent', body: back ? 'The agent has not answered yet.' : 'The agent has not said anything about it yet.', meta }
  }
}
