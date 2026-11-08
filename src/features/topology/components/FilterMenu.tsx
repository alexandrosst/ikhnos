import { Filter as FilterIcon, X } from 'lucide-react'
import { Button, ICON_SM, MenuPanel } from '@/components/ui/primitives'
import { DEFAULT_KINDS, filterActive, filterSummary, NO_APP, SERVICE_KINDS, type Filter } from '@/features/topology/lib/filter'
import type { Application, Cluster, ServiceKind } from '@/lib/types'

/** What the Kind section offers: the services together, then the machinery around them on request. */
const KIND_CHOICES: { id: string; name: string; hint?: string; kinds: ServiceKind[] }[] = [
  { id: 'services', name: 'Services', hint: 'deployments, stateful sets', kinds: DEFAULT_KINDS },
  { id: 'DaemonSet', name: 'DaemonSets', hint: 'one per node', kinds: ['DaemonSet'] },
  { id: 'Job', name: 'Jobs', hint: 'run once', kinds: ['Job'] },
]

/**
 * "Show only these": a popover with a checklist of clusters, one of applications, and one of workload kinds
 * (the services by default; DaemonSets and Jobs are ticked in on request). Nothing chosen in the cluster or
 * application list means all of it. The filter lives in the URL, so a filtered view can be linked and saved.
 *
 * Controlled (`open`/`onOpenChange`) rather than managing its own state: the Topology toolbar has several of
 * these popovers side by side (this one, options), and only one of them may be open at a
 * time - a page-level switch enforces that, click-outside and Escape included, so two never fight over the screen.
 */
export default function FilterMenu({
  open,
  onOpenChange,
  filter,
  clusters,
  applications,
  onChange,
  disabled,
}: {
  disabled?: boolean
  open: boolean
  onOpenChange: (open: boolean) => void
  filter: Filter
  clusters: Cluster[]
  applications: Application[]
  onChange: (f: Filter) => void
}) {
  const count = filter.clusters.length + filter.apps.length + (filter.kinds.length ? 1 : 0)
  // Kinds read the other way round from the lists above: nothing chosen is every kind, so what is ticked is what is shown.
  const shownKinds = new Set<ServiceKind>(filter.kinds.length ? filter.kinds : SERVICE_KINDS)
  const toggleKinds = (kinds: ServiceKind[]) => {
    const next = new Set(shownKinds)
    const on = kinds.every((k) => next.has(k))
    for (const k of kinds) {
      if (on) next.delete(k)
      else next.add(k)
    }
    if (next.size === 0) return // something has to be shown
    const chosen = SERVICE_KINDS.filter((k) => next.has(k))
    onChange({ ...filter, kinds: chosen.length === SERVICE_KINDS.length ? [] : chosen })
  }

  const toggle = (key: 'clusters' | 'apps' | 'kinds', id: string) => {
    const cur = filter[key]
    onChange({ ...filter, [key]: cur.includes(id as never) ? cur.filter((x) => x !== id) : [...cur, id] } as Filter)
  }

  const list = (key: 'clusters' | 'apps' | 'kinds', items: { id: string; name: string; hint?: string }[], empty: string) => (
    <div>
      {items.length === 0 && <p className="px-3 py-2 text-xs text-nb-500">{empty}</p>}
      {items.map((it) => (
        <label key={it.id} className="flex cursor-pointer items-center gap-2 rounded-md px-3 py-1.5 text-sm text-nb-300 hover:bg-nb-940">
          <input type="checkbox" className="accent-[var(--color-accent,#f68330)]" checked={(filter[key] as string[]).includes(it.id)} onChange={() => toggle(key, it.id)} data-testid={`filter-${key}-${it.id}`} />
          <span className="min-w-0 flex-1 truncate">{it.name}</span>
          {it.hint && <span className="shrink-0 text-xs text-nb-500">{it.hint}</span>}
        </label>
      ))}
    </div>
  )

  return (
    <div className="relative">
      <Button onClick={() => onOpenChange(!open)} disabled={disabled} aria-haspopup="dialog" aria-expanded={open} aria-label="Filter the topology" data-testid="filter-button">
        <FilterIcon size={ICON_SM} className={filterActive(filter) ? 'text-accent' : ''} />
        <span>Filter</span>
        {count > 0 && (
          <span className="rounded-full bg-accent/20 px-1.5 text-xs text-accent" data-testid="filter-count">
            {count}
          </span>
        )}
      </Button>
      <MenuPanel open={open} onClose={() => onOpenChange(false)} className="w-80 overflow-hidden" role="dialog" aria-label="Filter the topology" data-testid="filter-menu">
        <div className="flex items-center justify-between border-b border-nb-850 px-3 py-2">
          <span className="text-sm font-medium text-nb-200">Show only…</span>
          <button
            className="flex items-center gap-1 rounded px-1.5 py-0.5 text-xs text-nb-400 hover:bg-nb-850 hover:text-nb-200 disabled:opacity-40"
            disabled={!filterActive(filter)}
            onClick={() => onChange({ clusters: [], apps: [], kinds: [] })}
            data-testid="filter-clear"
          >
            <X size={ICON_SM} /> Clear
          </button>
        </div>
        <div className="max-h-[26rem] overflow-y-auto p-1">
          <div className="px-3 pb-1 pt-2 text-xs uppercase tracking-wide text-nb-500">Clusters</div>
          {list('clusters', clusters.map((c) => ({ id: c.id, name: c.name, hint: c.tier })), 'No clusters yet.')}
          <div className="px-3 pb-1 pt-3 text-xs uppercase tracking-wide text-nb-500">Applications</div>
          {list('apps', [...applications.map((a) => ({ id: a.id, name: a.name })), { id: NO_APP, name: 'No application', hint: 'unassigned' }], '')}
          <div className="px-3 pb-1 pt-3 text-xs uppercase tracking-wide text-nb-500">Workloads</div>
          {KIND_CHOICES.map((c) => (
            <label key={c.id} className="flex cursor-pointer items-center gap-2 rounded-md px-3 py-1.5 text-sm text-nb-300 hover:bg-nb-940">
              <input type="checkbox" className="accent-[var(--color-accent,#f68330)]" checked={c.kinds.every((k) => shownKinds.has(k))} onChange={() => toggleKinds(c.kinds)} data-testid={`filter-kinds-${c.id}`} />
              <span className="min-w-0 flex-1 truncate">{c.name}</span>
              {c.hint && <span className="shrink-0 text-xs text-nb-500">{c.hint}</span>}
            </label>
          ))}
        </div>
        <p className="border-t border-nb-850 px-3 py-2 text-xs text-nb-500">Nothing ticked under Clusters or Applications means all of them. A dependency is shown only if both ends are.</p>
      </MenuPanel>
    </div>
  )
}

/** What the filter is doing, said next to the button ("Deployments only"), so a default or a shared link that hides
 *  things is never a mystery; the cross clears it. Nothing when nothing is filtered. */
export function FilterChip({ filter, onClear }: { filter: Filter; onClear: () => void }) {
  const text = filterSummary(filter)
  if (!text) return null
  return (
    <span className="inline-flex h-8 max-w-48 items-center gap-1 rounded-lg bg-accent-soft pl-2.5 pr-1 text-xs text-accent" data-testid="filter-chip">
      <span className="truncate" title={text}>{text}</span>
      <button type="button" onClick={onClear} className="grid size-5 shrink-0 place-items-center rounded hover:bg-accent/20" aria-label={`Clear the filter (${text})`} data-testid="filter-chip-clear">
        <X size={12} />
      </button>
    </span>
  )
}
