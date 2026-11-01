import type { GatewayTraffic, SignalTraffic } from '@/lib/generated/wire'
import type { LinkState } from '@/lib/linkTone'

/** What one store is doing, from what the central gateway counted over the last few minutes (its own counters, which it pushes to FUSION). */
export type TrafficState = 'receiving' | 'quiet' | 'held' | 'failing'

export interface TrafficWords {
  state: TrafficState
  /** One or two words: Receiving, Quiet, Held up, Not receiving. */
  word: string
  /** The figures, in words; empty when there is nothing to say. */
  text: string
}

export type Kind = 'metrics' | 'logs' | 'traces'
const NOUN: Record<Kind, string> = { metrics: 'samples', logs: 'log lines', traces: 'spans' }

/** A rate per second as a person would say it: "20 samples/s" from one a second, "30 log lines/min" from one a minute, else per hour. */
export function rateWord(perSecond: number, noun: string): string {
  if (perSecond >= 10) return `${Math.round(perSecond).toLocaleString('en')} ${noun}/s`
  if (perSecond >= 1) return `${perSecond.toFixed(1)} ${noun}/s`
  const m = perSecond * 60
  if (m >= 10) return `${Math.round(m)} ${noun}/min`
  if (m >= 1) return `${m.toFixed(1)} ${noun}/min`
  const h = perSecond * 3600
  if (h >= 1) return `${Math.round(h)} ${noun}/h`
  return `under 1 ${noun.replace(/s$/, '')}/h`
}

const FULL = 0.8

/**
 * What a store is doing. Receiving: the gateway delivered something to it lately. Quiet: nothing was sent and nothing failed (that is
 * normal while the senders are idle). Held up: it is receiving but some failed or were turned away, or the queue to it is nearly full.
 * Not receiving: everything that was sent to it failed.
 */
export function storeTraffic(kind: Kind, t: SignalTraffic): TrafficWords {
  const failed = t.failed ?? 0
  const refused = t.refused ?? 0
  const full = (t.queue ?? 0) >= FULL
  const noun = NOUN[kind]
  if (failed > 0 && t.sent <= 0) return { state: 'failing', word: 'Not receiving', text: `${rateWord(failed, noun)} failed to be delivered` }
  if (failed > 0 || refused > 0 || full) {
    const bits = [t.sent > 0 ? `${rateWord(t.sent, noun)} delivered` : '']
    if (failed > 0) bits.push(`${rateWord(failed, noun)} failed`)
    if (refused > 0) bits.push(`${rateWord(refused, noun)} turned away`)
    if (full) bits.push(`queue ${Math.round((t.queue ?? 0) * 100)}% full`)
    return { state: 'held', word: 'Held up', text: bits.filter(Boolean).join(', ') }
  }
  if (t.sent > 0) return { state: 'receiving', word: 'Receiving', text: `${rateWord(t.sent, noun)}` }
  return { state: 'quiet', word: 'Quiet', text: 'nothing in the last few minutes' }
}

/** The arrow into a store: moving only while the store is being delivered to. */
export const trafficLink = (s: TrafficState): LinkState => (s === 'receiving' ? 'flowing' : s === 'held' ? 'stalled' : s === 'failing' ? 'broken' : 'unknown')

export const TRAFFIC_TONE: Record<TrafficState, string> = { receiving: 'text-ok', held: 'text-warn', failing: 'text-bad', quiet: 'text-nb-500' }

/** What came in at the gateway, kind by kind, in one line; the state is Held up while it turned any away. */
export function gatewayTraffic(t: GatewayTraffic): { state: TrafficState; word: string; text: string; moving: boolean } {
  const kinds: Kind[] = ['metrics', 'logs', 'traces']
  const parts = kinds.filter((k) => t[k].accepted > 0).map((k) => `${rateWord(t[k].accepted, NOUN[k])}`)
  const refused = kinds.filter((k) => (t[k].refused ?? 0) > 0).map((k) => `${rateWord(t[k].refused ?? 0, NOUN[k])}`)
  const moving = parts.length > 0
  if (refused.length > 0) return { state: 'held', word: 'Turning data away', text: [moving ? `${parts.join(', ')} taken in` : '', `${refused.join(', ')} turned away`].filter(Boolean).join('; '), moving }
  if (moving) return { state: 'receiving', word: 'Taking in', text: parts.join(', '), moving }
  return { state: 'quiet', word: 'Quiet', text: 'nothing came in during the last few minutes', moving }
}
