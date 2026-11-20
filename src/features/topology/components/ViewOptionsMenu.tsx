import clsx from 'clsx'
import { ScanEye, SlidersHorizontal } from 'lucide-react'
import { ICON_SM, MenuButton, MenuPanel, Select } from '@/components/ui/primitives'
import { Switch } from '@/components/ui/Switch'
import type { ViewOptions } from '@/features/topology/useTopologyOptions'

// `lens`, when true, marks a control that changes what the canvas is actually interpreting or computing (recolouring edges by a different
// live signal, e.g.) rather than just drawing or hiding something already decided - see the "Lenses" subsection below, where every
// lens-type Toggle in this menu lives together under its own label, the same way this menu already splits "Show" from "Layout". It renders
// the small ScanEye glyph and keeps the switch itself the same accent colour on, so the distinction reads at a glance without the page
// growing a second control shape to learn.
function Toggle({ checked, onChange, label, disabled, title, lens }: { checked: boolean; onChange: (v: boolean) => void; label: string; disabled?: boolean; title?: string; lens?: boolean }) {
  return (
    <label
      title={title}
      className={clsx('flex w-full items-center gap-2.5 whitespace-nowrap rounded-md px-2 py-1.5 text-left text-sm text-nb-300', disabled ? 'opacity-50' : 'cursor-pointer hover:bg-nb-930')}
    >
      <Switch size="sm" checked={checked} disabled={disabled} onChange={onChange} label={label} />
      {lens && <ScanEye size={ICON_SM} className="shrink-0 text-accent" aria-hidden />}
      {label}
    </label>
  )
}

/**
 * The "Options" popover of the topology toolbar: what is drawn (Show), what the canvas reads (Lenses) and how it is laid out (Layout).
 * Each switch writes one parameter of the address (`setParam`; null takes it out); what the page must do besides - let go of a selected
 * thing that is no longer drawn - it is told through the two callbacks.
 */
export default function ViewOptionsMenu({ options, hasMesh, hasNoise, open, onToggle, onClose, disabled, setParam, onDevicesHidden, onGroupChanged }: {
  options: ViewOptions
  /** Whether any connected cluster runs a service mesh (the Service mesh lens means nothing without one). */
  hasMesh: boolean
  /** Whether any dependency is DNS or system noise (the switch for it is only offered then). */
  hasNoise: boolean
  open: boolean
  onToggle: () => void
  onClose: () => void
  disabled: boolean
  setParam: (key: string, value: string | null) => void
  /** Devices and external endpoints were switched off: a selected one is no longer drawn anywhere. */
  onDevicesHidden: () => void
  /** The grouping changed: what was selected is in a different place, or gone. */
  onGroupChanged: () => void
}) {
  const { mode, showDevices, showNoise, servicesOnNodes, links, showLabels, showMesh, showHealthLens, showNamespaces, showChain, groupBy } = options
  return (
    <div className="relative">
      <MenuButton icon={SlidersHorizontal} label="Options" collapse open={open} onClick={onToggle} disabled={disabled} aria-haspopup="true" aria-label="View options" data-testid="view-options" />
      <MenuPanel open={open} onClose={onClose} className="w-72 p-2" role="group" aria-label="View options">
        {/* Three subsections: what's drawn at all, what instead changes which signal the canvas
            is reading (a "lens" - recolouring/reinterpreting what's already drawn rather than
            adding or removing anything), then how it's all laid out. "Show"/"Layout" mirror
            FilterMenu's own subsection labels (same classes) so the toolbar's two popovers read as
            one family instead of two different menus; "Lenses" follows the identical pattern
            rather than inventing a new one, since a grouped, labelled subsection was already this
            page's own way of telling two kinds of option apart. */}
        <div className="px-2 pb-1 pt-1 text-xs uppercase tracking-wide text-nb-500">Show</div>
        {mode === 'application' && (
          <Toggle
            checked={showDevices}
            onChange={(v) => {
              setParam('devices', v ? null : '0')
              // Hiding devices removes the selected one from the canvas; Inspector would otherwise
              // keep showing (and let you edit) something no longer drawn anywhere.
              if (!v) onDevicesHidden()
            }}
            label="Devices and external endpoints"
          />
        )}
        {mode === 'application' && hasNoise && (
          <Toggle checked={showNoise} onChange={(v) => setParam('noise', v ? '1' : null)} label="DNS & system traffic" />
        )}
        {mode === 'infrastructure' && (
          <>
            <Toggle checked={servicesOnNodes} onChange={(v) => setParam('services', v ? '1' : null)} label="Services on nodes" />
            <Toggle checked={links} onChange={(v) => setParam('links', v ? null : '0')} label="Cross-cluster links" />
          </>
        )}
        <Toggle checked={showLabels} onChange={(v) => setParam('labels', v ? '1' : null)} label="Edge labels" />

        {/* Lenses: unlike every toggle above (which only ever decides whether something already
            computed gets drawn), each of these changes what the canvas itself is interpreting -
            Service mesh swaps an edge's colour from the plain loss/quality read to its mTLS
            verdict, and Network health colours a measured cluster link by its live loss% - so
            they get their own labelled group, and each
            Toggle below also carries the small ScanEye glyph (see Toggle's own `lens` prop) as a
            second, per-row cue for anyone who lands here without reading the section header.
            Network health applies in both modes (a measured cluster link is a network fact, not an
            application-view one), so only Service mesh's own row is further gated to the
            application view. */}
        <div className="mt-1 border-t border-nb-850 px-2 pb-1 pt-2.5 text-xs uppercase tracking-wide text-nb-500">Lenses</div>
        {mode === 'application' && (
          <>
            <Toggle
              lens
              checked={showMesh}
              disabled={!hasMesh}
              onChange={(v) => setParam('mesh', v ? '1' : null)}
              label="Service mesh"
              title={hasMesh ? 'Recolour each connection by its mTLS verdict instead of loss/quality, and show which services are in the mesh' : 'No service mesh was found in the connected clusters'}
            />
            {!hasMesh && <p className="-mt-0.5 px-2 pb-1 pl-[46px] text-[11px] text-nb-500">No mesh found in your clusters</p>}
          </>
        )}
        <Toggle
          lens
          checked={showHealthLens}
          onChange={(v) => setParam('health', v ? '1' : null)}
          label="Network health"
          title="Colour the measured line between clusters by its live loss%"
        />

        <div className="mt-1 border-t border-nb-850 px-2 pb-1 pt-2.5 text-xs uppercase tracking-wide text-nb-500">Layout</div>
        {mode === 'application' && (
          <Toggle
            checked={showNamespaces}
            disabled={groupBy !== 'cluster' || showChain}
            onChange={(v) => setParam('namespaces', v ? '1' : null)}
            label="Namespace sub-boxes"
            title={showChain ? 'Not available in chain layout' : groupBy === 'cluster' ? 'Draw a box per namespace inside each cluster' : 'Only available grouped by cluster'}
          />
        )}
        {mode === 'application' && !showChain && groupBy !== 'cluster' && (
          <p className="-mt-0.5 px-2 pb-1 pl-[46px] text-[11px] text-nb-500">Only available grouped by cluster</p>
        )}
        {mode === 'application' && (
          <Toggle
            checked={showChain}
            onChange={(v) => setParam('chain', v ? '1' : null)}
            label="Chain layout"
            title="Lay every service out left to right by who calls whom, across every cluster, instead of grouping them into boxes"
          />
        )}
        {mode === 'application' && showChain && (
          <p className="-mt-0.5 px-2 pb-1 pl-[46px] text-[11px] text-nb-500">Namespace sub-boxes and Group by aren&apos;t available while this is on</p>
        )}
        <div className={clsx('mt-1 flex items-center justify-between gap-3 px-2 py-1.5 text-sm', showChain ? 'text-nb-600' : 'text-nb-400')}>
          Group by
          <Select
            className="h-8 w-32"
            value={groupBy}
            disabled={showChain}
            title={showChain ? 'Not available in chain layout' : undefined}
            onChange={(e) => {
              setParam('group', e.target.value === 'tier' ? 'tier' : null)
              onGroupChanged()
            }}
          >
            <option value="cluster">Cluster</option>
            <option value="tier">Tier</option>
          </Select>
        </div>
      </MenuPanel>
    </div>
  )
}
