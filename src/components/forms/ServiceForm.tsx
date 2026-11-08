import { Plus, Trash2 } from 'lucide-react'
import { useMemo, useState, type ReactNode } from 'react'
import { EditAction, FormRow, LabelChips, OriginLine, Row, Section, ViewFooter } from '@/components/entity/parts'
import { useFieldLocks } from '@/components/entity/useFieldLocks'
import { Button, GuessDot, ICON_MD, ICON_SM, Input, LabelsEditor, Modal, Select, StatusDot } from '@/components/ui/primitives'
import { effective, hasOverrides } from '@/lib/effective'
import { originOf } from '@/lib/fieldOrigin'
import { observation } from '@/lib/provenance'
import { uid, useRawTopology, useTopology } from '@/store/topology'
import { useServer } from '@/store/server'
import { DEFAULT_ORG, type Dependency, type Service, type ServiceKind } from '@/lib/types'
import { StatusSelect, DiscoveredNote } from './shared'

/** Fields that are yours by nature: an agent never reports them, so they are never locked. */
const YOURS = ['sensitivity', 'labels']

const SENSITIVITY = { public: 'Public', internal: 'Internal', confidential: 'Confidential' } as const

/**
 * A service in a side panel: read-only first, with a mark for where each fact comes from; Edit turns it into a form where a value an agent
 * reports stays locked until you choose Override. `edit` opens straight into the form; a new service always does. The calls it makes to
 * other services are edited here too.
 */
export function ServiceForm({ initial, onClose, defaultClusterId, edit, onDelete }: { initial: Service | null; onClose: () => void; defaultClusterId?: string; edit?: boolean; onDelete?: () => void }) {
  const { clusters, nodes, services, dependencies, applications, saveService, resetOverrides, upsertDependency, deleteDependency } = useTopology()
  const by = useServer((s) => s.user?.username)
  const raw = useRawTopology((s) => (initial ? s.services.find((w) => w.id === initial.id) : undefined))
  const current = raw ? effective(raw) : initial
  const [editing, setEditing] = useState(!initial || !!edit)
  const [f, setF] = useState<Service>(
    () =>
      current ?? {
        id: uid('w'),
        orgId: DEFAULT_ORG,
        name: '',
        namespace: 'default',
        clusterId: defaultClusterId ?? clusters[0]?.id ?? '',
        kind: 'Deployment',
        image: '',
        replicas: 1,
        nodeIds: [],
        status: 'unknown',
        labels: {},
        source: 'manual',
      },
  )
  const [labels, setLabels] = useState(f.labels)
  // This form edits service → service calls; calls to devices or external endpoints are left untouched.
  const isEditable = (d: Dependency) => d.from === f.id && d.fromKind === 'service' && d.toKind === 'service'
  const [deps, setDeps] = useState<Dependency[]>(() => dependencies.filter(isEditable))
  const set = <K extends keyof Service>(k: K, v: Service[K]) => setF((p) => ({ ...p, [k]: v }))
  const { weak, lock, reopen } = useFieldLocks({ kind: 'service', id: initial?.id, raw, form: f, setForm: setF, yours: YOURS, by })

  const clusterNodes = useMemo(() => nodes.filter((n) => n.clusterId === f.clusterId), [nodes, f.clusterId])
  const targets = services.filter((w) => w.id !== f.id)
  const clusterName = (id: string) => clusters.find((c) => c.id === id)?.name
  const nodeName = (id: string) => nodes.find((n) => n.id === id)?.name ?? id
  const appName = (id?: string) => applications.find((a) => a.id === id)?.name

  const startEditing = () => {
    if (!current) return
    setF(current)
    setLabels(current.labels)
    setDeps(dependencies.filter(isEditable))
    reopen(raw)
    setEditing(true)
  }
  const done = () => (initial ? setEditing(false) : onClose())
  const save = () => {
    saveService({ ...f, name: f.name.trim(), labels }, by)
    const kept = new Set(deps.map((d) => d.id))
    dependencies.filter((d) => isEditable(d) && !kept.has(d.id)).forEach((d) => deleteDependency(d.id))
    deps.filter((d) => d.to).forEach((d) => upsertDependency({ ...d, from: f.id }))
    done()
  }

  /* ---------- read-only ---------- */
  if (!editing && current) {
    const w = current
    const obs = observation(w)
    const calls = dependencies.filter(isEditable)
    const row = (label: string, field: string, value: ReactNode, mono?: boolean) => {
      const g = weak(w as never, field)
      return (
        <Row label={label} origin={originOf(raw, field)} by={raw?.overrideMeta?.[field]?.by} mono={mono}>
          {value === undefined || value === null || value === '' || value === false ? undefined : (
            <>
              {value}
              {g && <span className="ml-1.5 align-middle"><GuessDot level={g.level} why={g.why} /></span>}
            </>
          )}
        </Row>
      )
    }
    return (
      <Modal
        open
        side
        onClose={onClose}
        title={w.name}
        description={<OriginLine rec={w} createdAt={w.createdAt} />}
        headerActions={<EditAction onClick={startEditing} />}
        footer={<ViewFooter onDelete={onDelete} />}
      >
        <dl>
          <Section title="Identity">
            {row('Name', 'name', w.name)}
            {row('Cluster', 'clusterId', clusterName(w.clusterId))}
            {row('Namespace', 'namespace', w.namespace)}
            {row('Kind', 'kind', w.kind)}
            {row('Application', 'applicationId', appName(w.applicationId))}
            <Row label="Status" origin={originOf(raw, 'status')} by={raw?.overrideMeta?.status?.by}>
              <StatusDot status={w.status} withLabel notCurrent={obs && obs.kind !== 'live' ? (obs.reason ?? obs.label) : undefined} />
            </Row>
          </Section>
          <Section title="Runtime">
            {row('Image', 'image', w.image, true)}
            {row('Replicas', 'replicas', w.readyReplicas !== undefined ? `${w.readyReplicas} of ${w.replicas} ready` : String(w.replicas))}
            {row('Ports', 'ports', (w.ports ?? []).join(', '), true)}
            {row('Runs on', 'nodeIds', w.nodeIds.map(nodeName).join(', '))}
          </Section>
          <Section title="Policy">
            {row('Data sensitivity', 'sensitivity', w.sensitivity && SENSITIVITY[w.sensitivity])}
          </Section>
          <Section title="Calls">
            {calls.length === 0 ? (
              <Row label="Calls" />
            ) : (
              calls.map((d) => {
                const t = services.find((x) => x.id === d.to)
                return (
                  <Row key={d.id} label={t?.name ?? d.to}>
                    {[clusterName(t?.clusterId ?? ''), [d.protocol, d.port].filter(Boolean).join(':')].filter(Boolean).join(' · ')}
                  </Row>
                )
              })
            )}
          </Section>
          <Section title="Labels">
            <Row label="Labels" origin={originOf(raw, 'labels')} by={raw?.overrideMeta?.labels?.by}>
              {Object.keys(w.labels ?? {}).length > 0 ? <LabelChips labels={w.labels} /> : undefined}
            </Row>
          </Section>
        </dl>
      </Modal>
    )
  }

  /* ---------- form ---------- */
  return (
    <Modal
      open
      side
      onClose={onClose}
      title={initial ? `Edit ${current?.name ?? 'service'}` : 'Add service'}
      description={initial ? undefined : 'A microservice / application running in a cluster, plus the services it calls.'}
      footer={
        <>
          {initial && hasOverrides(initial) && (
            <Button
              variant="ghost"
              className="mr-auto"
              onClick={() => {
                resetOverrides('service', initial.id)
                setEditing(false)
              }}
            >
              Reset to detected values
            </Button>
          )}
          <Button onClick={done}>Cancel</Button>
          <Button variant="primary" disabled={!f.name.trim() || !f.clusterId} onClick={save}>
            {initial ? 'Save changes' : 'Add'}
          </Button>
        </>
      }
    >
      <DiscoveredNote entity={raw ?? null} />
      <FormRow label="Name" {...lock('name', f.name)}>
        <Input autoFocus value={f.name} onChange={(e) => set('name', e.target.value)} placeholder="e.g. inference-edge" />
      </FormRow>
      <FormRow label="Cluster" {...lock('clusterId', clusterName(f.clusterId))}>
        <Select value={f.clusterId} onChange={(e) => setF((p) => ({ ...p, clusterId: e.target.value, nodeIds: [] }))}>
          {clusters.length === 0 && <option value="">No clusters yet</option>}
          {clusters.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Namespace" {...lock('namespace', f.namespace)}>
        <Input value={f.namespace} onChange={(e) => set('namespace', e.target.value)} />
      </FormRow>
      <FormRow label="Kind" {...lock('kind', f.kind)}>
        <Select value={f.kind} onChange={(e) => set('kind', e.target.value as ServiceKind)}>
          {(['Deployment', 'StatefulSet', 'DaemonSet', 'Job'] as const).map((k) => (
            <option key={k}>{k}</option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Application" hint="Groups related services. Manage under Applications." {...lock('applicationId', appName(f.applicationId))}>
        <Select value={f.applicationId ?? ''} onChange={(e) => set('applicationId', e.target.value || undefined)}>
          <option value="">No application</option>
          {applications.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Image" {...lock('image', f.image)}>
        <Input value={f.image} onChange={(e) => set('image', e.target.value)} placeholder="ghcr.io/org/app:1.0" />
      </FormRow>
      <FormRow label="Replicas" {...lock('replicas', String(f.replicas))}>
        <Input type="number" min={0} value={f.replicas} onChange={(e) => set('replicas', Number(e.target.value))} />
      </FormRow>
      <FormRow label="Status" {...lock('status', <span className="capitalize">{f.status}</span>)}>
        <StatusSelect value={f.status} onChange={(v) => set('status', v)} />
      </FormRow>
      <FormRow label="Data sensitivity" hint="Used by placement policy: confidential services should not leave trusted sites.">
        <Select value={f.sensitivity ?? ''} onChange={(e) => set('sensitivity', (e.target.value || undefined) as Service['sensitivity'])}>
          <option value="">Not set</option>
          <option value="public">Public</option>
          <option value="internal">Internal</option>
          <option value="confidential">Confidential</option>
        </Select>
      </FormRow>
      <FormRow label="Runs on nodes" group {...lock('nodeIds', f.nodeIds.map(nodeName).join(', '))}>
        {clusterNodes.length === 0 ? (
          <p className="pt-2 text-xs text-nb-500">This cluster has no nodes yet. Add nodes to place the service.</p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {clusterNodes.map((n) => {
              const on = f.nodeIds.includes(n.id)
              return (
                <button
                  key={n.id}
                  type="button"
                  aria-pressed={on}
                  onClick={() => set('nodeIds', on ? f.nodeIds.filter((x) => x !== n.id) : [...f.nodeIds, n.id])}
                  className={'rounded-md border px-2.5 py-1 text-xs transition-colors ' + (on ? 'border-accent/50 bg-accent-soft text-accent' : 'border-nb-800 bg-nb-925 text-nb-400 hover:bg-nb-940')}
                >
                  {n.name}
                </button>
              )
            })}
          </div>
        )}
      </FormRow>
      <FormRow label="Labels">
        <LabelsEditor value={labels} onChange={setLabels} keyPlaceholder="tier" valuePlaceholder="backend" />
      </FormRow>

      <div className="mt-4 rounded-lg border border-nb-850 bg-nb-930 p-4">
        <div className="mb-3 flex items-center justify-between">
          <div>
            <div className="text-sm font-medium text-nb-300">Calls (outgoing dependencies)</div>
            <div className="text-xs text-nb-500">Drawn as edges in the Application view.</div>
          </div>
          <Button
            size="sm"
            disabled={targets.length === 0}
            onClick={() => setDeps((d) => [...d, { id: uid('d'), orgId: DEFAULT_ORG, from: f.id, fromKind: 'service', to: targets[0].id, toKind: 'service', protocol: 'HTTP', sources: ['manual'], confidence: 'high' }])}
          >
            <Plus size={ICON_SM} /> Add
          </Button>
        </div>
        {deps.length === 0 && <p className="text-xs text-nb-500">No dependencies.</p>}
        <div className="space-y-2">
          {deps.map((d, i) => {
            const patch = (p: Partial<Dependency>) => setDeps((all) => all.map((x, j) => (j === i ? { ...x, ...p } : x)))
            return (
              <div key={d.id} className="grid grid-cols-[1fr_96px_72px_36px] gap-2">
                <Select value={d.to} onChange={(e) => patch({ to: e.target.value })} aria-label="Calls">
                  {targets.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name} · {clusterName(t.clusterId) ?? '?'}
                    </option>
                  ))}
                </Select>
                <Input value={d.protocol} onChange={(e) => patch({ protocol: e.target.value })} placeholder="Protocol" aria-label="Protocol" />
                <Input type="number" value={d.port ?? ''} onChange={(e) => patch({ port: e.target.value ? Number(e.target.value) : undefined })} placeholder="Port" aria-label="Port" />
                <Button variant="ghost" className="h-9 px-0" aria-label="Remove dependency" onClick={() => setDeps((all) => all.filter((_, j) => j !== i))}>
                  <Trash2 size={ICON_MD} />
                </Button>
              </div>
            )
          })}
        </div>
      </div>
    </Modal>
  )
}
