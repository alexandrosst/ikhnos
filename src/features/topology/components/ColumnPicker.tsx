import { Columns3 } from 'lucide-react'
import { useState } from 'react'
import { Button, ICON_SM, MenuPanel } from '@/components/ui/primitives'
import type { ColumnDef, ColumnState } from '@/features/topology/lib/columns'

/**
 * "Which columns show": a small popover checklist next to a list page's search/filter row, so a dense
 * table (Nodes, Clusters, Applications, ...) can be trimmed to what one person actually looks at without
 * opening every row's edit form to see the rest. Self-contained (its own open state), unlike Topology's
 * toolbar popovers - a list page has only this one, so there is no "only one open at a time" to coordinate.
 */
export default function ColumnPicker({ columns, state }: { columns: ColumnDef[]; state: Pick<ColumnState, 'isVisible' | 'toggle'> & Partial<Pick<ColumnState, 'reset' | 'changed'>> }) {
  const [open, setOpen] = useState(false)
  const { isVisible, toggle: onToggle, reset, changed } = state

  return (
    <div className="relative">
      <Button onClick={() => setOpen((o) => !o)} aria-haspopup="dialog" aria-expanded={open} aria-label="Choose columns" data-testid="columns-button">
        <Columns3 size={ICON_SM} />
        <span>Columns</span>
      </Button>
      <MenuPanel open={open} onClose={() => setOpen(false)} className="w-64 overflow-hidden" role="dialog" aria-label="Choose columns" data-testid="columns-menu">
        <div className="flex items-center justify-between border-b border-nb-850 px-3 py-2">
          <span className="text-sm font-medium text-nb-200">Columns</span>
          {changed && reset && (
            <button type="button" onClick={reset} className="rounded px-1.5 py-0.5 text-xs text-nb-400 hover:bg-nb-850 hover:text-nb-200" data-testid="columns-reset">
              Reset
            </button>
          )}
        </div>
        <div className="max-h-[22rem] overflow-y-auto p-1">
          {columns.map((c) => (
            <label key={c.key} className="flex cursor-pointer items-center gap-2 rounded-md px-3 py-1.5 text-sm text-nb-300 hover:bg-nb-940">
              <input type="checkbox" className="accent-[var(--color-accent,#f68330)]" checked={isVisible(c.key)} onChange={() => onToggle(c.key)} data-testid={`column-${c.key}`} />
              <span className="min-w-0 flex-1 truncate">{c.label}</span>
            </label>
          ))}
        </div>
      </MenuPanel>
    </div>
  )
}
