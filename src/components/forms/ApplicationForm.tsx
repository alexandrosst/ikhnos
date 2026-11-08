import { useState } from 'react'
import { EditAction, FormRow, OriginLine, Row, Section, ViewFooter } from '@/components/entity/parts'
import { GroupingPicker } from '@/components/GroupingPicker'
import { Button, Input, Modal } from '@/components/ui/primitives'
import { originLabel } from '@/lib/present'
import { groupingAlternativesFor } from '@/lib/suggestions'
import { uid, useRawTopology, useTopology } from '@/store/topology'
import { DEFAULT_ORG, type Application } from '@/lib/types'

/**
 * An application in a side panel: read-only first (what it groups and how the grouping was decided), then a form. An application is a
 * grouping you declared or accepted, which no agent observes, so nothing in it is locked.
 */
export function ApplicationForm({ initial, onClose, edit, onDelete }: { initial: Application | null; onClose: () => void; edit?: boolean; onDelete?: () => void }) {
  const upsert = useTopology((s) => s.upsertApplication)
  const regroup = useTopology((s) => s.regroupApplication)
  const suggestions = useTopology((s) => s.suggestions)
  const services = useTopology((s) => s.services)
  const devices = useTopology((s) => s.devices)
  const clusters = useTopology((s) => s.clusters)
  const stored = useRawTopology((s) => (initial ? s.applications.find((a) => a.id === initial.id) : undefined))
  const current = stored ?? initial
  const [editing, setEditing] = useState(!initial || !!edit)
  const [f, setF] = useState<Application>(() => current ?? { id: uid('app'), orgId: DEFAULT_ORG, name: '', description: '', origin: 'explicit', confidence: 'high', source: 'manual' })
  const alternatives = initial ? groupingAlternativesFor(initial, suggestions) : []
  const done = () => (initial ? setEditing(false) : onClose())

  if (!editing && current) {
    const a = current
    const mine = services.filter((s) => s.applicationId === a.id)
    const units = devices.filter((d) => d.applicationId === a.id).reduce((n, d) => n + d.count, 0)
    const where = [...new Set(mine.map((s) => clusters.find((c) => c.id === s.clusterId)?.name).filter(Boolean))].join(', ')
    return (
      <Modal
        open
        side
        onClose={onClose}
        title={a.name}
        description={<OriginLine rec={a} />}
        headerActions={<EditAction onClick={() => { setF(a); setEditing(true) }} />}
        footer={<ViewFooter onDelete={onDelete} legend={false} />}
      >
        <dl>
          <Section title="Identity">
            <Row label="Name">{a.name}</Row>
            <Row label="Description">{a.description}</Row>
            <Row label="Grouped by">{originLabel(a.origin)}</Row>
          </Section>
          <Section title="Members">
            <Row label="Services">{mine.length > 0 ? mine.map((s) => s.name).join(', ') : undefined}</Row>
            <Row label="Devices">{units > 0 ? `${units} ${units === 1 ? 'unit' : 'units'}` : undefined}</Row>
            <Row label="Clusters">{where || undefined}</Row>
          </Section>
        </dl>
      </Modal>
    )
  }

  return (
    <Modal
      open
      side
      onClose={onClose}
      title={initial ? `Edit ${current?.name ?? 'application'}` : 'Add application'}
      description={initial ? undefined : 'A group of services and devices that together deliver something. Assign them from the Services and Devices pages.'}
      footer={
        <>
          <Button onClick={done}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!f.name.trim()}
            onClick={() => {
              upsert({ ...f, name: f.name.trim() })
              done()
            }}
          >
            {initial ? 'Save changes' : 'Add'}
          </Button>
        </>
      }
    >
      <FormRow label="Name">
        <Input autoFocus value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="e.g. Sensor ingestion" />
      </FormRow>
      <div className="pl-[9.25rem]">
        <GroupingPicker
          label="Group by a different label instead"
          alternatives={alternatives}
          onPick={(alt) => {
            regroup(initial!.id, alt)
            onClose()
          }}
        />
      </div>
      <FormRow label="Description">
        <Input value={f.description} onChange={(e) => setF({ ...f, description: e.target.value })} />
      </FormRow>
    </Modal>
  )
}
