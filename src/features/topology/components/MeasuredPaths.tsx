import clsx from 'clsx'
import { CircleAlert, Gauge, Plus, Trash2 } from 'lucide-react'
import { useMemo, useState } from 'react'
import { Button, Field, ICON_MD, ICON_SM, Input, Pill, Select, Table, Td, Th } from '@/components/ui/primitives'
import type { ProbeTarget } from '@/lib/history'
import { hasRoundTrip } from '@/lib/metrics'
import { ago } from '@/lib/observed'
import { atLeast } from '@/lib/api'
import { useConn, useServer } from '@/store/server'
import { useSettings } from '@/store/settings'
import { usePaths, useTopology } from '@/store/topology'

const ms = (n: number) => (n < 10 ? n.toFixed(1) : Math.round(n).toString())

/** How many rows a list shows before the rest is asked for. */
const SHOWN = 10

/** The first `SHOWN` of a list, and a button under it that shows all of it (or folds it back). Nothing when the list fits. */
function useFirst<T>(list: readonly T[], what: string) {
  const [all, setAll] = useState(false)
  const shown = all ? list : list.slice(0, SHOWN)
  const more =
    list.length > SHOWN ? (
      <div className="mt-2 flex items-center gap-3 text-xs text-nb-500" data-testid={`${what}-more`}>
        <span>{all ? `All ${list.length}` : `Showing ${SHOWN} of ${list.length}`}</span>
        <Button variant="ghost" size="sm" onClick={() => setAll(!all)} data-testid={`${what}-toggle`}>
          {all ? 'Show fewer' : `Show all ${list.length}`}
        </Button>
      </div>
    ) : null
  return { shown, more }
}

/**
 * Round-trip times and loss between clusters and the places they talk to, measured by the agents (TCP connect
 * timing, opt-in, no probes beyond an address the cluster already uses or an administrator named).
 */
export default function MeasuredPaths() {
  const paths = usePaths()
  const { agents, clusters } = useTopology()
  const status = useServer((s) => s.status)
  const admin = useServer((s) => atLeast(s.role, 'admin'))
  const measuring = agents.filter((a) => (a.measuring ?? 0) > 0)
  // Within a cluster, the slowest first; a path with no round trip at all has no time to sort by and goes last.
  const ordered = useMemo(
    () => [...paths].sort((a, b) => a.fromName.localeCompare(b.fromName) || Number(hasRoundTrip(b)) - Number(hasRoundTrip(a)) || b.rttP50Ms - a.rttP50Ms),
    [paths],
  )

  const { shown, more } = useFirst(ordered, 'paths')

  return (
    <section className="mt-10" aria-label="Measured paths" data-testid="measured-paths">
      <div className="mb-3 flex flex-wrap items-center gap-3">
        <h2 className="flex items-center gap-2 text-lg font-medium text-nb-300">
          <Gauge size={ICON_SM} className="text-accent" aria-hidden /> Measured paths
        </h2>
        {status === 'connected' && (
          <Pill>
            {measuring.length === 0 ? 'no agent is measuring' : `${measuring.length} agent${measuring.length === 1 ? '' : 's'} measuring`}
          </Pill>
        )}
      </div>
      <p className="mb-4 max-w-3xl text-sm text-nb-500">
        How long a connection takes to open from a cluster to the places it talks to, and how often it fails. The placement advice uses these instead of guesses where they exist. Measuring is off until you turn it on for an agent (in the connect wizard, or with the agent’s measurement setting). It only opens connections to the addresses you ask for below, and, if you turn that on there, to the places the clusters call most.
      </p>

      {ordered.length === 0 ? (
        <div className="rounded-xl border border-dashed border-nb-850 px-5 py-8 text-center text-sm text-nb-400" data-testid="no-paths">
          Nothing has been measured yet.{' '}
          {status !== 'connected' ? 'Paths are measured by agents reporting to an Ikhnos server.' : measuring.length === 0 ? 'Enable measurements on an agent to start.' : 'Results appear within a couple of minutes.'}
        </div>
      ) : (
        <Table>
          <thead>
            <tr>
              <Th>From</Th>
              <Th>To</Th>
              <Th>Round trip (median)</Th>
              <Th>Best · 95th</Th>
              <Th>Failed</Th>
              <Th>Samples</Th>
              <Th>Measured</Th>
            </tr>
          </thead>
          <tbody>
            {shown.map((p) => (
              <tr key={p.id} className={clsx(p.stale && 'opacity-60')} data-testid="path-row">
                <Td className="font-medium text-nb-300">{p.fromName}</Td>
                <Td>
                  <div>{p.toName ?? p.label ?? p.host}</div>
                  <div className="font-mono text-xs text-nb-500">{p.host}:{p.port} · {p.source === 'manual' ? 'you asked' : 'seen in traffic'}</div>
                </Td>
                {hasRoundTrip(p) ? (
                  <>
                    <Td className="tabular-nums text-nb-300">{ms(p.rttP50Ms)} ms</Td>
                    <Td className="tabular-nums text-nb-400">{ms(p.rttMinMs)} · {ms(p.rttP95Ms)} ms</Td>
                  </>
                ) : (
                  <Td colSpan={2} className="text-nb-500" data-testid="path-no-roundtrip">
                    {p.samples > 0 ? 'no connection completed' : 'not measured yet'}
                  </Td>
                )}
                <Td className={clsx('tabular-nums', p.lossPct > 0 ? 'text-warn' : 'text-nb-400')}>{p.lossPct > 0 ? `${p.lossPct.toFixed(0)} %` : 'none'}</Td>
                <Td className="tabular-nums text-nb-400">{p.samples}</Td>
                <Td className="whitespace-nowrap text-nb-400">{p.stale ? <span className="text-warn">stale · </span> : null}{ago(p.at)}</Td>
              </tr>
            ))}
          </tbody>
        </Table>
      )}
      {more}

      {status === 'connected' && <Targets admin={!!admin} clusterOptions={agents.filter((a) => a.clusterId).map((a) => a.clusterId as string)} clusters={clusters} />}
    </section>
  )
}

function Targets({ admin, clusterOptions, clusters }: { admin: boolean; clusterOptions: string[]; clusters: { id: string; name: string }[] }) {
  const conn = useConn()
  const { settings, save, error } = useSettings()
  const [cluster, setCluster] = useState('')
  const [host, setHost] = useState('')
  const [port, setPort] = useState('443')
  const [label, setLabel] = useState('')
  const [busy, setBusy] = useState(false)
  const options = clusters.filter((c) => clusterOptions.includes(c.id))
  const name = (id: string) => clusters.find((c) => c.id === id)?.name ?? id
  const p = Number(port)
  const valid = cluster !== '' && host.trim() !== '' && Number.isInteger(p) && p >= 1 && p <= 65535

  const { shown: shownTargets, more: moreTargets } = useFirst(settings.probeTargets, 'targets')

  const write = async (targets: ProbeTarget[], measureObserved = settings.measureObserved) => {
    setBusy(true)
    try {
      return await save(conn, { ...settings, probeTargets: targets, measureObserved })
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-6 rounded-xl border border-nb-850 bg-nb-925 p-5">
      <h3 className="mb-1 text-sm font-medium text-nb-300">Addresses you asked to be measured</h3>
      <p className="mb-3 text-xs text-nb-500">For places no cluster talks to yet, such as a site you plan to move something to. Loopback, link-local and metadata addresses are always refused.</p>
      <label className="mb-3 flex items-start gap-2.5 text-sm">
        <input
          type="checkbox"
          className="mt-0.5 size-4 accent-[var(--color-accent)]"
          checked={settings.measureObserved}
          disabled={!admin || busy}
          onChange={(e) => void write(settings.probeTargets, e.target.checked)}
          data-testid="measure-observed"
        />
        <span>
          <span className="text-nb-300">Also measure the places clusters call most</span>
          <span className="block text-xs text-nb-500">Off by default. When on, the server picks the outside addresses each cluster’s traffic shows most (for agents at access level 2 or above that allow measuring) and has the agent time them, so rows marked “seen in traffic” appear below. Only the addresses you add here are measured otherwise.</span>
        </span>
      </label>
      {settings.probeTargets.length === 0 ? (
        <p className="text-sm text-nb-400">None.</p>
      ) : (
        <ul className="divide-y divide-nb-850/60 text-sm" data-testid="probe-targets">
          {shownTargets.map((t) => (
            <li key={t.id} className="flex items-center gap-3 py-2">
              <span className="text-nb-300">{name(t.clusterId)}</span>
              <span className="text-nb-600">→</span>
              <span className="font-mono text-xs text-nb-300">{t.host}:{t.port}</span>
              {t.label && <span className="text-nb-500">{t.label}</span>}
              {admin && (
                <Button variant="ghost" size="sm" className="ml-auto" disabled={busy} onClick={() => write(settings.probeTargets.filter((x) => x.id !== t.id))} aria-label={`Stop measuring ${t.host}`}>
                  <Trash2 size={ICON_MD} />
                </Button>
              )}
            </li>
          ))}
        </ul>
      )}
      {moreTargets}
      {error && <p className="mt-3 text-sm text-bad" role="alert"><CircleAlert size={ICON_SM} className="mr-1 inline" aria-hidden />{error}</p>}
      {admin ? (
        <div className="mt-4 grid gap-3 border-t border-nb-850 pt-4 md:grid-cols-[1fr_1.4fr_6rem_1fr_auto] md:items-end">
          <Field label="Measured from">
            <Select value={cluster} onChange={(e) => setCluster(e.target.value)} placeholder="Choose a cluster" aria-label="Measured from">
              {options.map((c) => (
                <option key={c.id} value={c.id}>{c.name}</option>
              ))}
            </Select>
          </Field>
          <Field label="Address">
            <Input value={host} onChange={(e) => setHost(e.target.value)} placeholder="10.20.0.5 or host.example.org" data-testid="probe-host" />
          </Field>
          <Field label="Port">
            <Input value={port} inputMode="numeric" onChange={(e) => setPort(e.target.value)} data-testid="probe-port" />
          </Field>
          <Field label="Label (optional)">
            <Input value={label} onChange={(e) => setLabel(e.target.value)} maxLength={80} placeholder="Frankfurt edge" />
          </Field>
          <Button
            variant="primary"
            disabled={!valid || busy}
            data-testid="add-probe"
            onClick={async () => {
              const ok = await write([...settings.probeTargets, { id: '', clusterId: cluster, host: host.trim(), port: p, label: label.trim() }])
              if (ok) {
                setHost('')
                setLabel('')
              }
            }}
          >
            <Plus size={ICON_SM} /> Measure
          </Button>
        </div>
      ) : (
        <p className="mt-3 text-xs text-nb-500">Only administrators can add or remove these.</p>
      )}
    </div>
  )
}
