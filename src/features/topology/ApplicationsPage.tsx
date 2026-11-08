import { Plus } from 'lucide-react'
import { useState } from 'react'
import ColumnPicker from '@/features/topology/components/ColumnPicker'
import { ApplicationForm, ConfirmModal } from '@/components/forms'
import { OriginLine } from '@/components/entity/parts'
import { Button, ChipList, EmptyState, ICON_SM, PageHeader, Pill, Table, Td, Th } from '@/components/ui/primitives'
import { useColumnVisibility, type ColumnDef } from '@/features/topology/lib/columns'
import { originLabel } from '@/lib/present'
import type { Application } from '@/lib/types'
import { useTopology } from '@/store/topology'
import { matches, openOnClick, RowActions, SearchBox } from './shared'

const APPLICATION_COLUMNS: ColumnDef[] = [
  { key: 'description', label: 'Description' },
  { key: 'services', label: 'Services' },
  { key: 'devices', label: 'Devices' },
  { key: 'clusters', label: 'Clusters' },
  { key: 'grouped', label: 'Grouped by' },
]

export default function ApplicationsPage() {
  const { applications, services, devices, clusters, deleteApplication } = useTopology()
  const [q, setQ] = useState('')
  // The panel: an application to read (or edit straight away from the pencil), or a blank one to add.
  const [panel, setPanel] = useState<{ app: Application | null; edit: boolean } | null>(null)
  const [deleting, setDeleting] = useState<Application | null>(null)
  const { isVisible, toggle } = useColumnVisibility('applications')

  const rows = applications.filter((a) => matches(q, a.name, a.description))

  return (
    <>
      <PageHeader
        title="Applications"
        description="Services and devices grouped into the things your users care about. Groupings are declared by you or accepted from a suggestion; no agent observes them."
        actions={
          <Button variant="primary" onClick={() => setPanel({ app: null, edit: true })}>
            <Plus size={ICON_SM} /> Add application
          </Button>
        }
      />
      <div className="mb-4 flex flex-wrap gap-3">
        <SearchBox value={q} onChange={setQ} placeholder="Search applications…" />
        <ColumnPicker columns={APPLICATION_COLUMNS} isVisible={isVisible} onToggle={toggle} />
      </div>

      {applications.length === 0 ? (
        <EmptyState
          title="No applications yet"
          description="No grouping exists yet. Discovery suggests one once agents report services, or create an application and assign services to it from the Services page."
          action={<Button variant="primary" onClick={() => setPanel({ app: null, edit: true })}><Plus size={ICON_SM} /> Add application</Button>}
        />
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>Name</Th>
              {isVisible('services') && <Th>Services</Th>}
              {isVisible('devices') && <Th>Devices</Th>}
              {isVisible('clusters') && <Th>Clusters</Th>}
              {isVisible('grouped') && <Th>Grouped by</Th>}
              <Th className="sticky right-0 bg-nb-925" />
            </tr>
          </thead>
          <tbody>
            {rows.map((a) => {
              const mine = services.filter((s) => s.applicationId === a.id)
              const cIds = [...new Set(mine.map((s) => s.clusterId))]
              return (
                <tr key={a.id} className="group cursor-pointer hover:bg-nb-930/60" {...openOnClick(() => setPanel({ app: a, edit: false }))}>
                  <Td className="whitespace-nowrap">
                    <button className="font-medium text-nb-300 hover:text-accent" onClick={() => setPanel({ app: a, edit: false })}>{a.name}</button>
                    <OriginLine rec={a} />
                  </Td>
                  {isVisible('description') && <Td className="max-w-xs truncate text-nb-400" title={a.description}>{a.description || <span className="text-nb-600">—</span>}</Td>}
                  {isVisible('services') && <Td>{mine.length}</Td>}
                  {isVisible('devices') && <Td>{devices.filter((d) => d.applicationId === a.id).reduce((s, d) => s + d.count, 0) || '—'}</Td>}
                  {isVisible('clusters') && <Td><ChipList items={cIds.map((id) => clusters.find((c) => c.id === id)?.name).filter((n): n is string => !!n)} /></Td>}
                  {isVisible('grouped') && <Td><Pill title={a.origin}>{originLabel(a.origin)}</Pill></Td>}
                  <Td className="sticky right-0 bg-nb-925 group-hover:bg-nb-930"><RowActions onEdit={() => setPanel({ app: a, edit: true })} onDelete={() => setDeleting(a)} /></Td>
                </tr>
              )
            })}
            {rows.length === 0 && (
              <tr><Td colSpan={2 + APPLICATION_COLUMNS.filter((c) => isVisible(c.key)).length} className="py-8 text-center text-nb-500">No applications match “{q}”.</Td></tr>
            )}
          </tbody>
        </Table>
      )}

      {panel && (
        <ApplicationForm
          key={panel.app?.id ?? 'new'}
          initial={panel.app}
          edit={panel.edit}
          onClose={() => setPanel(null)}
          onDelete={panel.app ? () => { setDeleting(panel.app); setPanel(null) } : undefined}
        />
      )}
      {deleting && (
        <ConfirmModal
          title={`Delete ${deleting.name}?`}
          message="Its services are kept; they just stop belonging to an application."
          onConfirm={() => deleteApplication(deleting.id)}
          onClose={() => setDeleting(null)}
        />
      )}
    </>
  )
}
