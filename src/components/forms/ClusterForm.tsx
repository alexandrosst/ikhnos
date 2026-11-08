import { Pencil, Trash2 } from 'lucide-react'
import { useState, type ReactNode } from 'react'
import { DistroIcon, ProviderIcon, WithIcon } from '@/components/ui/brand'
import { Button, ComboField, GuessDot, ICON_SM, Input, LabelsEditor, Modal, Select, StatusDot, TierBadge } from '@/components/ui/primitives'
import { FormRow, OriginLine, Row, Section } from '@/components/entity/parts'
import { completeness } from '@/lib/completeness'
import { effective, hasOverrides } from '@/lib/effective'
import { useFieldLocks } from '@/components/entity/useFieldLocks'
import { originOf } from '@/lib/fieldOrigin'
import { CNI_OPTIONS, DISTRIBUTION_OPTIONS, PROVIDER_OPTIONS } from '@/lib/present'
import { observation } from '@/lib/provenance'
import { uid, useRawTopology, useTopology } from '@/store/topology'
import { useServer } from '@/store/server'
import { DEFAULT_ORG, TIERS, type Cluster } from '@/lib/types'
import { StatusSelect, TrustSelect, DiscoveredNote } from './shared'

/** Fields that are yours by nature: an agent never reports them, so they are never locked. */
const YOURS = ['siteId', 'labels', 'trustZone', 'dataResidency']

const blank = (): Cluster => ({
  id: uid('cl'),
  orgId: DEFAULT_ORG,
  name: '',
  tier: 'cloud',
  distribution: 'kubeadm',
  version: '',
  provider: '',
  region: '',
  status: 'unknown',
  labels: {},
  source: 'manual',
})

/**
 * A cluster in a side panel. It opens read-only, every fact with a mark for where it comes from, and Edit turns it into a form. In the form a
 * value an agent reports is locked until you choose Override, so you only ever change what you mean to change; the rest stays tracked.
 * `edit` opens straight into the form (the pencil in a list); a new cluster always does.
 */
export function ClusterForm({ initial, onClose, edit, onDelete }: { initial: Cluster | null; onClose: () => void; edit?: boolean; onDelete?: () => void }) {
  const save = useTopology((s) => s.saveCluster)
  const resetOverrides = useTopology((s) => s.resetOverrides)
  const sites = useTopology((s) => s.sites)
  const nodes = useTopology((s) => s.nodes)
  const services = useTopology((s) => s.services)
  const dependencies = useTopology((s) => s.dependencies)
  const by = useServer((s) => s.user?.username)
  // The stored record, before overrides: what an agent reported, and which fields are yours. The panel shows the live one, so a save is visible at once.
  const raw = useRawTopology((s) => (initial ? s.clusters.find((c) => c.id === initial.id) : undefined))
  const current = raw ? effective(raw) : initial
  const [editing, setEditing] = useState(!initial || !!edit)
  const [f, setF] = useState<Cluster>(() => current ?? blank())
  const [labels, setLabels] = useState(f.labels)
  const set = <K extends keyof Cluster>(k: K, v: Cluster[K]) => setF((p) => ({ ...p, [k]: v }))
  const { weak, lock, reopen } = useFieldLocks({ kind: 'cluster', id: initial?.id, raw, form: f, setForm: setF, yours: YOURS, by })

  const startEditing = () => {
    if (!current) return
    setF(current)
    setLabels(current.labels)
    reopen(raw)
    setEditing(true)
  }
  const done = () => (initial ? setEditing(false) : onClose())

  /* ---------- read-only ---------- */
  if (!editing && current) {
    const c = current
    const obs = observation(c)
    const gaps = completeness(c, nodes, services, dependencies).missing
    const site = sites.find((x) => x.id === c.siteId)
    const row = (label: string, field: string, value: ReactNode, mono?: boolean) => {
      const w = weak(c as never, field)
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
        title={c.name}
        description={
          <>
            <OriginLine rec={c} createdAt={c.createdAt} />
            {gaps.length > 0 && <p className="mt-1 text-xs text-nb-500">Not known yet: {gaps.join(', ')}.</p>}
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
            {row('Name', 'name', c.name)}
            {row('Tier', 'tier', <TierBadge tier={c.tier} />)}
            <Row label="Status" origin={originOf(raw, 'status')} by={raw?.overrideMeta?.status?.by}>
              <StatusDot status={c.status} withLabel notCurrent={obs && obs.kind !== 'live' ? (obs.reason ?? obs.label) : undefined} />
            </Row>
          </Section>
          <Section title="Platform">
            {row('Distribution', 'distribution', c.distribution && <WithIcon icon={<DistroIcon distribution={c.distribution} />}>{c.distribution}</WithIcon>)}
            {row('Version', 'version', c.version, true)}
            {row('Provider', 'provider', c.provider && <WithIcon icon={<ProviderIcon provider={c.provider} />}>{c.provider}</WithIcon>)}
            {row('CNI', 'cni', c.cni)}
            {row('Ingress', 'ingress', c.ingress)}
            {row('Storage classes', 'storageClasses', (c.storageClasses ?? []).join(', '))}
          </Section>
          <Section title="Network">
            {row('Pod CIDR', 'podCidr', c.podCidr, true)}
            {row('Service CIDR', 'serviceCidr', c.serviceCidr, true)}
            {row('API endpoint', 'apiEndpoint', c.apiEndpoint, true)}
            {row('Exit IP', 'egressIp', c.egressIp, true)}
          </Section>
          <Section title="Location">
            {row('Site', 'siteId', site?.name)}
            {row('Region label', 'region', c.region)}
          </Section>
          <Section title="Policy">
            {row('Trust zone', 'trustZone', c.trustZone && c.trustZone[0].toUpperCase() + c.trustZone.slice(1))}
            {row('Data residency', 'dataResidency', c.dataResidency)}
            <Row label="Labels" origin={originOf(raw, 'labels')} by={raw?.overrideMeta?.labels?.by}>
              {Object.keys(c.labels ?? {}).length > 0 && (
                <span className="flex flex-wrap gap-1.5">
                  {Object.entries(c.labels).map(([k, v]) => (
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
      title={initial ? `Edit ${current?.name ?? 'cluster'}` : 'Add cluster'}
      description={initial ? undefined : 'A Kubernetes cluster and where it sits on the cloud–edge continuum.'}
      footer={
        <>
          {initial && hasOverrides(initial) && (
            <Button
              variant="ghost"
              className="mr-auto"
              onClick={() => {
                resetOverrides('cluster', initial.id)
                setEditing(false)
              }}
            >
              Reset to detected values
            </Button>
          )}
          <Button onClick={done}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!f.name.trim()}
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
        <Input autoFocus value={f.name} onChange={(e) => set('name', e.target.value)} placeholder="e.g. edge-patras" />
      </FormRow>
      <FormRow label="Tier" hint="Drives vertical placement in the topology." {...lock('tier', TIERS.find((t) => t.value === f.tier)?.label)}>
        <Select value={f.tier} onChange={(e) => set('tier', e.target.value as Cluster['tier'])}>
          {TIERS.map((t) => (
            <option key={t.value} value={t.value}>
              {t.label}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Status" {...lock('status', <span className="capitalize">{f.status}</span>)}>
        <StatusSelect value={f.status} onChange={(v) => set('status', v)} />
      </FormRow>
      <FormRow label="Distribution" {...lock('distribution', f.distribution)}>
        <ComboField value={f.distribution} onChange={(v) => set('distribution', v)} options={DISTRIBUTION_OPTIONS} placeholder="EKS, k3s, kubeadm…" />
      </FormRow>
      <FormRow label="Version" {...lock('version', f.version)}>
        <Input value={f.version} onChange={(e) => set('version', e.target.value)} placeholder="v1.30.2" />
      </FormRow>
      <FormRow label="Provider" {...lock('provider', f.provider)}>
        <ComboField value={f.provider} onChange={(v) => set('provider', v)} options={PROVIDER_OPTIONS} placeholder="AWS, On-prem…" />
      </FormRow>
      <FormRow label="CNI" {...lock('cni', f.cni)}>
        <ComboField value={f.cni ?? ''} onChange={(v) => set('cni', v || undefined)} options={CNI_OPTIONS} placeholder="calico, flannel, cilium…" />
      </FormRow>
      <FormRow label="Region label" hint="A cloud region code or a city name. If the cluster is not on a site yet, it is used to suggest where it is." {...lock('region', f.region)}>
        <Input value={f.region} onChange={(e) => set('region', e.target.value)} placeholder="eu-central-1" />
      </FormRow>
      <FormRow label="Site" hint="Where it sits on the map. Manage sites under Sites.">
        <Select value={f.siteId ?? ''} onChange={(e) => set('siteId', e.target.value || undefined)}>
          <option value="">No site</option>
          {sites.map((x) => (
            <option key={x.id} value={x.id}>
              {x.name}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Trust zone" hint="Policy input for placement.">
        <TrustSelect value={f.trustZone} onChange={(v) => set('trustZone', v)} />
      </FormRow>
      <FormRow label="Data residency" hint="Jurisdiction data must stay in, e.g. EU.">
        <Input value={f.dataResidency ?? ''} onChange={(e) => set('dataResidency', e.target.value || undefined)} placeholder="EU" />
      </FormRow>
      <FormRow label="Labels">
        <LabelsEditor value={labels} onChange={setLabels} keyPlaceholder="env" valuePlaceholder="prod" />
      </FormRow>
    </Modal>
  )
}
