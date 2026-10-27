import type { AgentToolVersion } from '@/lib/consent'

const NAMES: Record<string, string> = { collector: 'Collector', kepler: 'Kepler', dcgm: 'DCGM exporter' }
const ENGINES: Record<string, string> = { ebpf: 'eBPF', powercap: 'powercap' }

/** A tool as it is shown: its name, the version it was installed with, and for Kepler its engine. A tool this page does not know keeps the name
 *  the chart gave it, so a newer chart's tool shows rather than disappears. */
export function toolLabel(t: AgentToolVersion): { name: string; version: string; engine?: string } {
  return {
    name: NAMES[t.name] ?? t.name,
    version: t.digestPinned ? 'pinned by digest' : t.version || 'not stated',
    engine: t.variant ? ENGINES[t.variant] ?? t.variant : undefined,
  }
}
