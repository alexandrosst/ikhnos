import { Plus, Trash2 } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { EditAction, FormRow, LabelChips, OriginLine, Row, Section, ViewFooter } from '@/components/entity/parts'
import { useFieldLocks } from '@/components/entity/useFieldLocks'
import { Button, ComboField, ICON_MD, ICON_SM, Input, LabelsEditor, Modal, Select, StatusDot } from '@/components/ui/primitives'
import { effective, hasOverrides } from '@/lib/effective'
import { originOf } from '@/lib/fieldOrigin'
import { DEVICE_PROTOCOL_OPTIONS } from '@/lib/present'
import { uid, useRawTopology, useTopology } from '@/store/topology'
import { useServer } from '@/store/server'
import { CONNECTIVITY, DEFAULT_ORG, DEVICE_KINDS, type Dependency, type Device, type DeviceKind } from '@/lib/types'
import { StatusSelect, ConnectivitySelect, DiscoveredNote } from './shared'

/** Fields that are yours by nature: where a device belongs and what is attached to it is your decision, not something it reports. */
const YOURS = ['applicationId', 'siteId', 'gatewayNodeId', 'labels']

/**
 * A device group in a side panel: read-only first, then a form. Devices are mostly declared, so most carry no marks; one an agent did
 * report keeps those fields locked until you choose Override. The services it sends data to are edited here too.
 */
export function DeviceForm({ initial, onClose, edit, onDelete }: { initial: Device | null; onClose: () => void; edit?: boolean; onDelete?: () => void }) {
  const { clusters, nodes, services, sites, applications, dependencies, saveDevice, resetOverrides, upsertDependency, deleteDependency } = useTopology()
  const by = useServer((s) => s.user?.username)
  const raw = useRawTopology((s) => (initial ? s.devices.find((d) => d.id === initial.id) : undefined))
  const current = raw ? effective(raw) : initial
  const [editing, setEditing] = useState(!initial || !!edit)
  const [f, setF] = useState<Device>(
    () =>
      current ?? {
        id: uid('dev'),
        orgId: DEFAULT_ORG,
        name: '',
        kind: 'sensor',
        count: 1,
        protocol: 'MQTT',
        connectivity: 'ethernet',
        status: 'unknown',
        labels: {},
        source: 'manual',
      },
  )
  const [labels, setLabels] = useState(f.labels)
  const isEditable = (d: Dependency) => d.from === f.id && d.fromKind === 'device' && d.toKind === 'service'
  const [deps, setDeps] = useState<Dependency[]>(() => dependencies.filter(isEditable))
  const set = <K extends keyof Device>(k: K, v: Device[K]) => setF((p) => ({ ...p, [k]: v }))
  const { lock, reopen } = useFieldLocks({ kind: 'device', id: initial?.id, raw, form: f, setForm: setF, yours: YOURS, by })
  const clusterName = (id: string) => clusters.find((c) => c.id === id)?.name ?? '?'
  const kindLabel = (k: string) => DEVICE_KINDS.find((x) => x.value === k)?.label ?? k
  const linkLabel = (c?: string) => CONNECTIVITY.find((x) => x.value === c)?.label

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
    saveDevice({ ...f, name: f.name.trim(), count: Math.max(1, Math.round(f.count) || 1), labels }, by)
    const kept = new Set(deps.map((d) => d.id))
    dependencies.filter((d) => isEditable(d) && !kept.has(d.id)).forEach((d) => deleteDependency(d.id))
    deps.filter((d) => d.to).forEach((d) => upsertDependency({ ...d, from: f.id }))
    done()
  }

  /* ---------- read-only ---------- */
  if (!editing && current) {
    const d = current
    const sends = dependencies.filter(isEditable)
    const row = (label: string, field: string, value: ReactNode, mono?: boolean) => (
      <Row label={label} origin={originOf(raw, field)} by={raw?.overrideMeta?.[field]?.by} mono={mono}>
        {value === undefined || value === null || value === '' || value === false ? undefined : value}
      </Row>
    )
    return (
      <Modal
        open
        side
        onClose={onClose}
        title={d.name}
        description={<OriginLine rec={d} />}
        headerActions={<EditAction onClick={startEditing} />}
        footer={<ViewFooter onDelete={onDelete} legend={d.source === 'discovered'} />}
      >
        <dl>
          <Section title="Identity">
            {row('Name', 'name', d.name)}
            {row('Kind', 'kind', kindLabel(d.kind))}
            {row('Units', 'count', String(d.count))}
            <Row label="Status" origin={originOf(raw, 'status')} by={raw?.overrideMeta?.status?.by}>
              <StatusDot status={d.status} withLabel />
            </Row>
          </Section>
          <Section title="Placement">
            {row('Application', 'applicationId', applications.find((a) => a.id === d.applicationId)?.name)}
            {row('Site', 'siteId', sites.find((s) => s.id === d.siteId)?.name)}
            {row('Attached to node', 'gatewayNodeId', nodes.find((n) => n.id === d.gatewayNodeId)?.name)}
          </Section>
          <Section title="Connection">
            {row('Protocol', 'protocol', d.protocol)}
            {row('Connectivity', 'connectivity', linkLabel(d.connectivity))}
            {row('Hardware model', 'hardwareModel', d.hardwareModel)}
            {row('Firmware', 'firmware', d.firmware, true)}
          </Section>
          <Section title="Sends data to">
            {sends.length === 0 ? (
              <Row label="Sends data to" />
            ) : (
              sends.map((x) => {
                const t = services.find((s) => s.id === x.to)
                return (
                  <Row key={x.id} label={t?.name ?? x.to}>
                    {[clusterName(t?.clusterId ?? ''), [x.protocol, x.port].filter(Boolean).join(':')].filter(Boolean).join(' · ')}
                  </Row>
                )
              })
            )}
          </Section>
          <Section title="Labels">
            <Row label="Labels" origin={originOf(raw, 'labels')} by={raw?.overrideMeta?.labels?.by}>
              {Object.keys(d.labels ?? {}).length > 0 ? <LabelChips labels={d.labels} /> : undefined}
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
      title={initial ? `Edit ${current?.name ?? 'device'}` : 'Add device'}
      description={initial ? undefined : 'Sensors, cameras, PLCs and other things that belong to an application but do not run on Kubernetes. Use one record for a fleet of identical units.'}
      footer={
        <>
          {initial && hasOverrides(initial) && (
            <Button
              variant="ghost"
              className="mr-auto"
              onClick={() => {
                resetOverrides('device', initial.id)
                setEditing(false)
              }}
            >
              Reset to detected values
            </Button>
          )}
          <Button onClick={done}>Cancel</Button>
          <Button variant="primary" disabled={!f.name.trim()} onClick={save}>
            {initial ? 'Save changes' : 'Add'}
          </Button>
        </>
      }
    >
      <DiscoveredNote entity={raw ?? null} />
      <FormRow label="Name" {...lock('name', f.name)}>
        <Input autoFocus value={f.name} onChange={(e) => set('name', e.target.value)} placeholder="e.g. Temperature sensors" />
      </FormRow>
      <FormRow label="Kind" {...lock('kind', kindLabel(f.kind))}>
        <Select value={f.kind} onChange={(e) => set('kind', e.target.value as DeviceKind)}>
          {DEVICE_KINDS.map((k) => (
            <option key={k.value} value={k.value}>
              {k.label}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Units" hint="How many identical devices this record stands for." {...lock('count', String(f.count))}>
        <Input type="number" min={1} value={f.count} onChange={(e) => set('count', Number(e.target.value))} />
      </FormRow>
      <FormRow label="Status" {...lock('status', <span className="capitalize">{f.status}</span>)}>
        <StatusSelect value={f.status} onChange={(v) => set('status', v)} />
      </FormRow>
      <FormRow label="Application">
        <Select value={f.applicationId ?? ''} onChange={(e) => set('applicationId', e.target.value || undefined)}>
          <option value="">No application</option>
          {applications.map((a) => (
            <option key={a.id} value={a.id}>
              {a.name}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Site" hint="Where the devices physically are.">
        <Select value={f.siteId ?? ''} onChange={(e) => set('siteId', e.target.value || undefined)}>
          <option value="">No site</option>
          {sites.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Protocol" {...lock('protocol', f.protocol)}>
        <ComboField value={f.protocol} onChange={(v) => set('protocol', v)} options={DEVICE_PROTOCOL_OPTIONS} placeholder="MQTT, OPC UA, Modbus, RTSP…" />
      </FormRow>
      <FormRow label="Connectivity" {...lock('connectivity', linkLabel(f.connectivity))}>
        <ConnectivitySelect value={f.connectivity} onChange={(v) => set('connectivity', v ?? 'unknown')} />
      </FormRow>
      <FormRow label="Attached to node" hint="Only for devices wired to a machine (USB / CSI camera, serial PLC).">
        <Select value={f.gatewayNodeId ?? ''} onChange={(e) => set('gatewayNodeId', e.target.value || undefined)}>
          <option value="">Not attached to a node</option>
          {nodes.map((n) => (
            <option key={n.id} value={n.id}>
              {n.name} · {clusterName(n.clusterId)}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Hardware model" {...lock('hardwareModel', f.hardwareModel)}>
        <Input value={f.hardwareModel ?? ''} onChange={(e) => set('hardwareModel', e.target.value || undefined)} />
      </FormRow>
      <FormRow label="Firmware" {...lock('firmware', f.firmware)}>
        <Input value={f.firmware ?? ''} onChange={(e) => set('firmware', e.target.value || undefined)} />
      </FormRow>
      <FormRow label="Labels">
        <LabelsEditor value={labels} onChange={setLabels} keyPlaceholder="line" valuePlaceholder="3" />
      </FormRow>

      <div className="mt-4 rounded-lg border border-nb-850 bg-nb-930 p-4">
        <div className="mb-3 flex items-center justify-between">
          <div>
            <div className="text-sm font-medium text-nb-300">Sends data to</div>
            <div className="text-xs text-nb-500">Usually a broker or ingest service. Drawn as edges in the Application view.</div>
          </div>
          <Button
            size="sm"
            disabled={services.length === 0}
            onClick={() => setDeps((d) => [...d, { id: uid('d'), orgId: DEFAULT_ORG, from: f.id, fromKind: 'device', to: services[0].id, toKind: 'service', protocol: f.protocol || 'MQTT', sources: ['manual'], confidence: 'high' }])}
          >
            <Plus size={ICON_SM} /> Add
          </Button>
        </div>
        {deps.length === 0 && <p className="text-xs text-nb-500">Not connected to any service.</p>}
        <div className="space-y-2">
          {deps.map((d, i) => {
            const patch = (p: Partial<Dependency>) => setDeps((all) => all.map((x, j) => (j === i ? { ...x, ...p } : x)))
            return (
              <div key={d.id} className="grid grid-cols-[1fr_96px_72px_36px] gap-2">
                <Select value={d.to} onChange={(e) => patch({ to: e.target.value })} aria-label="Sends to">
                  {services.map((t) => (
                    <option key={t.id} value={t.id}>
                      {t.name} · {clusterName(t.clusterId)}
                    </option>
                  ))}
                </Select>
                <Input value={d.protocol} onChange={(e) => patch({ protocol: e.target.value })} placeholder="Protocol" aria-label="Protocol" />
                <Input type="number" value={d.port ?? ''} onChange={(e) => patch({ port: e.target.value ? Number(e.target.value) : undefined })} placeholder="Port" aria-label="Port" />
                <Button variant="ghost" className="h-9 px-0" aria-label="Remove connection" onClick={() => setDeps((all) => all.filter((_, j) => j !== i))}>
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
