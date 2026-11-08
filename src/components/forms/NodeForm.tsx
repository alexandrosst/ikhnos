import { AlertTriangle, Pencil, Trash2 } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { FormRow, OriginLine, Row, Section } from '@/components/entity/parts'
import { useFieldLocks } from '@/components/entity/useFieldLocks'
import { Button, ComboField, GuessDot, ICON_SM, Input, LabelsEditor, Modal, Select, StatusDot } from '@/components/ui/primitives'
import { effective, hasOverrides } from '@/lib/effective'
import { originOf } from '@/lib/fieldOrigin'
import { accelSummary, formatCpu, formatMemory, OS_OPTIONS, podsLabel } from '@/lib/present'
import { observation } from '@/lib/provenance'
import { uid, useRawTopology, useTopology } from '@/store/topology'
import { useServer } from '@/store/server'
import { CONNECTIVITY, DEFAULT_ORG, type MachineNode } from '@/lib/types'
import { StatusSelect, ConnectivitySelect, DiscoveredNote } from './shared'

/** Fields that are yours by nature: an agent never reports them, so they are never locked. */
const YOURS = ['labels']

const KIND = { vm: 'Virtual machine', 'bare-metal': 'Bare metal', 'edge-device': 'Edge device' } as const

/**
 * A node in a side panel: read-only first, with a mark for where each fact comes from; Edit turns it into a form where a value an agent
 * reports stays locked until you choose Override. `edit` opens straight into the form; a new node always does.
 */
export function NodeForm({ initial, onClose, defaultClusterId, edit, onDelete }: { initial: MachineNode | null; onClose: () => void; defaultClusterId?: string; edit?: boolean; onDelete?: () => void }) {
  const clusters = useTopology((s) => s.clusters)
  const services = useTopology((s) => s.services)
  const save = useTopology((s) => s.saveNode)
  const resetOverrides = useTopology((s) => s.resetOverrides)
  const by = useServer((s) => s.user?.username)
  const raw = useRawTopology((s) => (initial ? s.nodes.find((n) => n.id === initial.id) : undefined))
  const current = raw ? effective(raw) : initial
  const [editing, setEditing] = useState(!initial || !!edit)
  const [f, setF] = useState<MachineNode>(
    () =>
      current ?? {
        id: uid('n'),
        orgId: DEFAULT_ORG,
        name: '',
        clusterId: defaultClusterId ?? clusters[0]?.id ?? '',
        role: 'worker',
        kind: 'vm',
        ip: '',
        os: 'Ubuntu 22.04',
        cpu: 4,
        memoryGb: 16,
        status: 'unknown',
        labels: {},
        source: 'manual',
      },
  )
  const [labels, setLabels] = useState(f.labels)
  const set = <K extends keyof MachineNode>(k: K, v: MachineNode[K]) => setF((p) => ({ ...p, [k]: v }))
  const { weak, lock, reopen } = useFieldLocks({ kind: 'node', id: initial?.id, raw, form: f, setForm: setF, yours: YOURS, by })

  const startEditing = () => {
    if (!current) return
    setF(current)
    setLabels(current.labels)
    reopen(raw)
    setEditing(true)
  }
  const done = () => (initial ? setEditing(false) : onClose())
  const clusterName = (id: string) => clusters.find((c) => c.id === id)?.name

  /* ---------- read-only ---------- */
  if (!editing && current) {
    const n = current
    const obs = observation(n)
    const placed = services.filter((w) => w.nodeIds.includes(n.id)).length
    const row = (label: string, field: string, value: ReactNode, mono?: boolean) => {
      const w = weak(n as never, field)
      return (
        <Row label={label} origin={originOf(raw, field)} by={raw?.overrideMeta?.[field]?.by} mono={mono}>
          {value === undefined || value === null || value === '' || value === false ? undefined : (
            <>
              {value}
              {w && <span className="ml-1.5 align-middle"><GuessDot level={w.level} why={w.why} /></span>}
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
        title={n.name}
        description={
          <>
            <OriginLine rec={n} createdAt={n.createdAt} />
            {n.conditions && n.conditions.length > 0 && (
              <p className="mt-1 flex items-center gap-1.5 text-xs text-warn">
                <AlertTriangle size={12} aria-hidden /> {n.conditions.join(', ')}
              </p>
            )}
          </>
        }
        headerActions={
          <Button size="sm" onClick={startEditing}>
            <Pencil size={ICON_SM} /> Edit
          </Button>
        }
        footer={
          <>
            {onDelete && (
              <Button variant="ghost" className="mr-auto" onClick={onDelete}>
                <Trash2 size={ICON_SM} /> Delete
              </Button>
            )}
            <span className="ml-auto text-xs text-nb-500">Lock: detected · Pencil: set by you</span>
          </>
        }
      >
        <dl>
          <Section title="Identity">
            {row('Name', 'name', n.name)}
            {row('Cluster', 'clusterId', clusterName(n.clusterId))}
            {row('Role', 'role', n.role === 'control-plane' ? 'Control plane' : 'Worker')}
            <Row label="Status" origin={originOf(raw, 'status')} by={raw?.overrideMeta?.status?.by}>
              <StatusDot status={n.status} withLabel notCurrent={obs && obs.kind !== 'live' ? (obs.reason ?? obs.label) : undefined} />
            </Row>
          </Section>
          <Section title="Hardware">
            {row('Machine type', 'kind', KIND[n.kind])}
            {row('Architecture', 'arch', n.arch)}
            {row('Hardware model', 'hardwareModel', n.hardwareModel)}
            {row('CPU', 'cpu', `${formatCpu(n.cpu)} vCPU`)}
            {row('Memory', 'memoryGb', formatMemory(n.memoryGb))}
            {row('Accelerators', 'accelerators', accelSummary(n.accelerators))}
            {row('Virtualization', 'virtualization', n.kind === 'vm' ? n.virtualization : undefined)}
          </Section>
          <Section title="System">
            {row('OS', 'os', n.os)}
            {row('Kernel', 'kernel', n.kernel, true)}
            {row('Runtime', 'runtime', n.runtime)}
            {row('Kubelet', 'kubeletVersion', n.kubeletVersion, true)}
          </Section>
          <Section title="Network">
            {row('IP address', 'ip', n.ip, true)}
            {row('Uplink', 'connectivity', CONNECTIVITY.find((c) => c.value === n.connectivity)?.label)}
            {row('Zone', 'zone', n.zone)}
            {row('Instance type', 'instanceType', n.instanceType)}
          </Section>
          <Section title="Workload">
            <Row label="Services">{placed > 0 ? `${placed} ${placed === 1 ? 'service' : 'services'}` : undefined}</Row>
            <Row label="Pods">{n.podCount !== undefined ? `${podsLabel(n.podCount, n.podCapacity)} pods` : undefined}</Row>
          </Section>
          <Section title="Labels">
            <Row label="Labels" origin={originOf(raw, 'labels')} by={raw?.overrideMeta?.labels?.by}>
              {Object.keys(n.labels ?? {}).length > 0 && (
                <span className="flex flex-wrap gap-1.5">
                  {Object.entries(n.labels).map(([k, v]) => (
                    <span key={k} className="rounded-md border border-nb-850 bg-nb-930 px-1.5 py-0.5 font-mono text-xs text-nb-400">
                      {k}={v}
                    </span>
                  ))}
                </span>
              )}
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
      title={initial ? `Edit ${current?.name ?? 'node'}` : 'Add node'}
      description={initial ? undefined : 'A machine (virtual machine, bare-metal server or edge device) that is part of a cluster.'}
      footer={
        <>
          {initial && hasOverrides(initial) && (
            <Button
              variant="ghost"
              className="mr-auto"
              onClick={() => {
                resetOverrides('node', initial.id)
                setEditing(false)
              }}
            >
              Reset to detected values
            </Button>
          )}
          <Button onClick={done}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!f.name.trim() || !f.clusterId}
            onClick={() => {
              save({ ...f, name: f.name.trim(), labels }, by)
              done()
            }}
          >
            {initial ? 'Save changes' : 'Add'}
          </Button>
        </>
      }
    >
      <DiscoveredNote entity={raw ?? null} />
      <FormRow label="Name" {...lock('name', f.name)}>
        <Input autoFocus value={f.name} onChange={(e) => set('name', e.target.value)} placeholder="e.g. worker-1" />
      </FormRow>
      <FormRow label="Cluster" {...lock('clusterId', clusterName(f.clusterId))}>
        <Select value={f.clusterId} onChange={(e) => set('clusterId', e.target.value)}>
          {clusters.length === 0 && <option value="">No clusters yet</option>}
          {clusters.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Role" {...lock('role', f.role === 'control-plane' ? 'Control plane' : 'Worker')}>
        <Select value={f.role} onChange={(e) => set('role', e.target.value as MachineNode['role'])}>
          <option value="worker">Worker</option>
          <option value="control-plane">Control plane</option>
        </Select>
      </FormRow>
      <FormRow label="Machine type" {...lock('kind', KIND[f.kind])}>
        <Select value={f.kind} onChange={(e) => set('kind', e.target.value as MachineNode['kind'])}>
          <option value="vm">Virtual machine</option>
          <option value="bare-metal">Bare metal</option>
          <option value="edge-device">Edge device</option>
        </Select>
      </FormRow>
      <FormRow label="IP address" {...lock('ip', f.ip)}>
        <Input value={f.ip} onChange={(e) => set('ip', e.target.value)} placeholder="10.0.0.10" />
      </FormRow>
      <FormRow label="OS" {...lock('os', f.os)}>
        <ComboField value={f.os} onChange={(v) => set('os', v)} options={OS_OPTIONS} placeholder="Ubuntu 22.04" />
      </FormRow>
      <FormRow label="CPU cores" {...lock('cpu', formatCpu(f.cpu))}>
        <Input type="number" min={1} value={f.cpu} onChange={(e) => set('cpu', Number(e.target.value))} />
      </FormRow>
      <FormRow label="Memory (GB)" {...lock('memoryGb', formatMemory(f.memoryGb))}>
        <Input type="number" min={1} value={f.memoryGb} onChange={(e) => set('memoryGb', Number(e.target.value))} />
      </FormRow>
      <FormRow label="Architecture" hint="Decides which images can run here." {...lock('arch', f.arch)}>
        <Select value={f.arch ?? ''} onChange={(e) => set('arch', e.target.value || undefined)}>
          <option value="">Unknown</option>
          {['amd64', 'arm64', 'arm', 'riscv64'].map((a) => (
            <option key={a}>{a}</option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Hardware model" {...lock('hardwareModel', f.hardwareModel)}>
        <Input value={f.hardwareModel ?? ''} onChange={(e) => set('hardwareModel', e.target.value || undefined)} placeholder="Raspberry Pi 5, Jetson Orin…" />
      </FormRow>
      <FormRow label="Uplink" {...lock('connectivity', CONNECTIVITY.find((c) => c.value === f.connectivity)?.label)}>
        <ConnectivitySelect value={f.connectivity} onChange={(v) => set('connectivity', v)} />
      </FormRow>
      <FormRow label="Status" {...lock('status', <span className="capitalize">{f.status}</span>)}>
        <StatusSelect value={f.status} onChange={(v) => set('status', v)} />
      </FormRow>
      <FormRow label="Labels">
        <LabelsEditor value={labels} onChange={setLabels} keyPlaceholder="accelerator" valuePlaceholder="jetson-orin" />
      </FormRow>
    </Modal>
  )
}
