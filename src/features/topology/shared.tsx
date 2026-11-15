import { Database, Pencil, Scaling, Search, ShieldCheck, Trash2 } from 'lucide-react'
import type { ReactNode } from 'react'
import ColumnPicker from '@/features/topology/components/ColumnPicker'
import type { ColumnDef, ColumnState } from '@/features/topology/lib/columns'
import { Button, ICON_MD, ICON_SM, Input, Trait } from '@/components/ui/primitives'
import { autoscalerRange, disruptionLabel, totalVolumeGb, volumeSize } from '@/lib/present'
import type { Service } from '@/lib/types'

export function SearchBox({ value, onChange, placeholder }: { value: string; onChange: (v: string) => void; placeholder: string }) {
  return (
    <div className="relative w-80">
      <Search size={ICON_MD} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-nb-500" />
      <Input className="pl-9" aria-label={placeholder} value={value} onChange={(e) => onChange(e.target.value)} placeholder={placeholder} />
    </div>
  )
}

/**
 * The row above every list page's table: search, the page's own filters (children), and the column picker. Over a list with
 * nothing in it none of the three does anything, so `empty` shows nothing and the page's empty state stands alone.
 */
export function ListToolbar({ q, onQ, placeholder, columns, state, empty, children }: { q: string; onQ: (v: string) => void; placeholder: string; columns: ColumnDef[]; state: ColumnState; empty?: boolean; children?: ReactNode }) {
  if (empty) return null
  return (
    <div className="mb-4 flex flex-wrap gap-3">
      <SearchBox value={q} onChange={onQ} placeholder={placeholder} />
      {children}
      <ColumnPicker columns={columns} state={state} />
    </div>
  )
}

/** Props for a table row that opens its panel on a click, unless the click lands on a control inside the row (a button, a link, a field). */
export const openOnClick = (onOpen: () => void) => ({
  onClick: (e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest('button, a, input, select, textarea, [role="button"], [role="combobox"]')) return
    if (window.getSelection()?.toString()) return
    onOpen()
  },
})

export function RowActions({ onEdit, onDelete }: { onEdit: () => void; onDelete: () => void }) {
  return (
    <div className="flex justify-end gap-1 opacity-50 transition-opacity focus-within:opacity-100 group-hover:opacity-100">
      <Button variant="ghost" size="sm" aria-label="Edit" title="Edit" onClick={onEdit}>
        <Pencil size={ICON_MD} />
      </Button>
      <Button variant="ghost" size="sm" aria-label="Delete" onClick={onDelete}>
        <Trash2 size={ICON_MD} />
      </Button>
    </div>
  )
}

export const matches = (q: string, ...fields: (string | undefined)[]) => {
  const s = q.trim().toLowerCase()
  return !s || fields.some((f) => f?.toLowerCase().includes(s))
}

/** Storage, autoscaling and disruption rules of a service as small chips: what an orchestrator must respect. */
export function ServiceTraits({ w, nodeName }: { w: Service; nodeName: (id: string) => string }) {
  const vols = w.volumes ?? []
  const pinned = vols.filter((v) => v.pinnedNodeIds?.length)
  if (!vols.length && !w.autoscaler && !w.disruption) return null
  return (
    <div className="mt-1 flex flex-wrap gap-1">
      {vols.length > 0 && (
        <Trait
          icon={<Database size={ICON_SM} />}
          warn={pinned.length > 0}
          title={vols.map((v) => `${v.name}: ${volumeSize(v.sizeGb)}${v.storageClass ? ` on ${v.storageClass}` : ''}${v.pinnedNodeIds?.length ? `, data only on ${v.pinnedNodeIds.map(nodeName).join(', ')}` : ''}`).join('\n')}
        >
          {volumeSize(totalVolumeGb(vols))}
          {pinned.length > 0 && ' · pinned'}
        </Trait>
      )}
      {w.autoscaler && (
        <Trait icon={<Scaling size={ICON_SM} />} title={`Autoscaled ${autoscalerRange(w.autoscaler)}, now ${w.autoscaler.current}${w.autoscaler.targets?.length ? `; target ${w.autoscaler.targets.join(', ')}` : ''}`}>
          {w.autoscaler.min}–{w.autoscaler.max}
        </Trait>
      )}
      {w.disruption && (
        <Trait icon={<ShieldCheck size={ICON_SM} />} title={`Disruption budget: ${disruptionLabel(w.disruption)}; ${w.disruption.allowed} pod(s) may be evicted right now`}>
          {disruptionLabel(w.disruption)}
        </Trait>
      )}
    </div>
  )
}
