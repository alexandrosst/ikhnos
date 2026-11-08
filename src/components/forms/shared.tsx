import { Select } from '@/components/ui/primitives'
import { CONNECTIVITY, type Connectivity, type Status, type TrustZone } from '@/lib/types'

const STATUSES: Status[] = ['healthy', 'degraded', 'offline', 'unknown']

export function StatusSelect({ value, onChange }: { value: Status; onChange: (s: Status) => void }) {
  return (
    <Select value={value} onChange={(e) => onChange(e.target.value as Status)}>
      {STATUSES.map((s) => (
        <option key={s} value={s}>
          {s[0].toUpperCase() + s.slice(1)}
        </option>
      ))}
    </Select>
  )
}

export function TrustSelect({ value, onChange }: { value?: TrustZone; onChange: (v: TrustZone | undefined) => void }) {
  return (
    <Select value={value ?? ''} onChange={(e) => onChange((e.target.value || undefined) as TrustZone | undefined)}>
      <option value="">Not set</option>
      <option value="public">Public</option>
      <option value="private">Private</option>
      <option value="restricted">Restricted</option>
    </Select>
  )
}

export function ConnectivitySelect({ value, onChange }: { value?: Connectivity; onChange: (v: Connectivity | undefined) => void }) {
  return (
    <Select value={value ?? ''} onChange={(e) => onChange((e.target.value || undefined) as Connectivity | undefined)}>
      <option value="">Not set</option>
      {CONNECTIVITY.map((c) => (
        <option key={c.value} value={c.value}>
          {c.label}
        </option>
      ))}
    </Select>
  )
}

/** Explains the two-layer values on entities that came from discovery. */
export function DiscoveredNote({ entity }: { entity: { source: string; overrides?: Record<string, unknown> } | null }) {
  if (!entity || entity.source !== 'discovered') return null
  const n = Object.keys(entity.overrides ?? {}).length
  return (
    <p className="mb-4 rounded-lg border border-nb-850 bg-nb-930 px-3.5 py-2.5 text-xs text-nb-400">
      Detected by discovery. Changes you make here are kept as overrides and survive rediscovery
      {n ? ` (${n} field${n === 1 ? '' : 's'} currently overridden).` : '.'}
    </p>
  )
}

