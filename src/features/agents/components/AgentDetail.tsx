import clsx from 'clsx'
import { ChevronRight } from 'lucide-react'
import { Fragment, type ReactNode } from 'react'
import { AccessPanel, DiscoveryChip, HealthChip, Problems } from '@/features/agents/components/AgentInsight'
import { AgentTelemetry } from '@/features/agents/components/AgentTelemetry'
import { AgentTeardown } from '@/features/agents/AgentTeardown'
import { CheckLine, MODULE_STYLE, ObserverLine, ScopeLine, when } from '@/features/discovery/components/AgentParts'
import EntityHistory from '@/features/history/components/EntityHistory'
import { Flag } from '@/components/ui/brand'
import { Button, GuessDot, SectionLabel } from '@/components/ui/primitives'
import { skewLabel, skewWarning } from '@/lib/clock'
import { uptimeWords, type AgentExtras } from '@/lib/consent'
import { ageOf } from '@/lib/history'
import { bytesTotal } from '@/lib/observed'
import { GEO_UNLOCATABLE_HELP, GEO_UNLOCATABLE_LABEL, ipScope, ipScopeLabel, withV } from '@/lib/present'
import { ACCESS_TIERS, type Agent } from '@/lib/types'
import { useServer } from '@/store/server'
import { CERT_SOON, inWords, msUntil, num, tierOf } from '@/features/agents/lib/agentView'

/** Everything the server knows about one agent that does not fit in a row. */
export function AgentDetail({ agent: a, extras, canConsent, onRevoke }: { agent: Agent; extras: AgentExtras; canConsent: boolean; onRevoke?: () => void }) {
  const l = a.link
  const certLeft = msUntil(a.certExpiresAt)
  const ip = a.connectingIp
  const publicIpFallbackOn = useServer((s) => s.info?.geoip?.publicIpFallback)
  const approved = a.status === 'approved'
  const d = approved ? extras.diagnostics : undefined
  const reported = a.status === 'approved' || a.status === 'revoked' || a.status === 'rejected'
  const modulesNotOk = a.modules.filter((m) => m.status !== 'ok')
  // The approved level, and only what differs from it: the install and the enrollment say more only when they are not the same.
  const tierName = (t?: number) => ACCESS_TIERS.find((x) => x.value === t)?.label
  const accessWords = [
    tierOf(a),
    a.installedTier !== undefined && a.installedTier !== a.accessTier ? `installed up to ${tierName(a.installedTier)}` : undefined,
    a.tierCap !== undefined && a.tierCap !== (a.installedTier ?? a.accessTier) ? `enrolled up to ${tierName(a.tierCap)}` : undefined,
  ].filter(Boolean).join(' · ')
  const connection = (
    <div data-testid="agent-connection">
      <Heading>Connection</Heading>
      <Facts
        rows={[
          ['From', ip ? `${ip}${ipScopeLabel(ipScope(ip)) ? ` (${ipScopeLabel(ipScope(ip)).toLowerCase()})` : ''}` : undefined],
          [
            'Located',
            a.connectingGeo ? (
              <span key="located" className="inline-flex flex-wrap items-center gap-x-2 gap-y-0.5">
                <span className="inline-flex items-center gap-2">
                  <Flag code={a.connectingGeo.country} />
                  <span>
                    {[a.connectingGeo.city, a.connectingGeo.countryName || a.connectingGeo.country].filter(Boolean).join(', ')}
                    {a.connectingGeo.estimated && <GuessDot why="Taken from this server's own internet connection, because the cluster's address is private." />}
                  </span>
                </span>
                {a.connectingGeo.asOrg && (
                  <span className="text-nb-500" title="Which network this address belongs to, from a separate ASN database.">
                    · {a.connectingGeo.asOrg}
                    {a.connectingGeo.asn ? ` (AS${a.connectingGeo.asn})` : ''}
                  </span>
                )}
              </span>
            ) : a.connectingGeoReason ? (
              <span key="located" className="text-nb-500" title={GEO_UNLOCATABLE_HELP[a.connectingGeoReason]}>
                No location — {GEO_UNLOCATABLE_LABEL[a.connectingGeoReason].toLowerCase()}
                {(a.connectingGeoReason === 'cgnat' || a.connectingGeoReason === 'private') && !publicIpFallbackOn &&
                  ' (an administrator can turn on estimating this from the server\'s own address in the deployment settings)'}
              </span>
            ) : undefined,
          ],
          ['Connected', l?.connectedSince ? ageOf(l.connectedSince) : a.connected === false ? 'not connected' : undefined],
          ['Certificate', a.certExpiresAt ? <span key="cert" className={certLeft !== undefined && certLeft < 0 ? 'text-bad' : certLeft !== undefined && certLeft < CERT_SOON && a.connected ? 'text-warn' : undefined} title={`Expires ${when(a.certExpiresAt)}`}>{certLeft !== undefined && certLeft < 0 ? `expired ${when(a.certExpiresAt)}` : certLeft !== undefined ? `${inWords(certLeft)} left` : `expires ${when(a.certExpiresAt)}`}</span> : undefined],
          // Only when it is off: a clock that is right says nothing, and the plain figure is in the technical details.
          ['Clock', a.clockSkewMs !== undefined && skewWarning(a.clockSkewMs) ? skewLabel(a.clockSkewMs) : undefined],
          ['Agent', d ? `${withV(d.agentVersion)}${d.arch ? ` · ${d.os ?? ''}/${d.arch}` : ''} · up ${uptimeWords(d.uptimeSeconds)}` : undefined],
          [
            'Watches',
            d && d.informers.length > 0 ? (
              <span key="watches">
                {d.informers.filter((i) => i.synced).length} of {d.informers.length} read
                {d.informers.filter((i) => !i.synced || i.lastError).map((i) => (
                  <span key={i.name} className="block break-words text-xs text-warn">{i.name}: {i.lastError || 'not read yet'}</span>
                ))}
              </span>
            ) : undefined,
          ],
        ]}
      />
    </div>
  )
  const access = approved && (d || canConsent) ? <AccessPanel agent={a} diagnostics={d} consent={extras.consent} editable={canConsent} /> : null
  return (
    <div className="space-y-6 text-sm">
      <div className="grid gap-x-10 gap-y-6 md:grid-cols-2" data-testid={d ? 'agent-health' : undefined}>
        {(d || access) && (
          <div className="space-y-4">
            {d && <div className="flex flex-wrap items-center gap-2"><HealthChip diagnostics={d} connected={a.connected} /><DiscoveryChip diagnostics={d} /><span className="text-xs text-nb-600">from the agent's own report, {ageOf(d.reportedAt)}</span></div>}
            {access}
            {d && d.problems.length > 0 && (
              <div>
                <Heading>Problems</Heading>
                <Problems problems={d.problems} />
              </div>
            )}
          </div>
        )}
        <div className="space-y-6">
          {connection}
          {approved && <AgentTelemetry agent={a} diagnostics={d} canConsent={canConsent} />}
        </div>
      </div>
      {(a.status === 'revoked' || a.status === 'rejected') && a.teardown && <AgentTeardown status={a.status} teardown={a.teardown} />}
      <Fold
        title="Technical details"
        testId="agent-technical"
        hint={modulesNotOk.length > 0 ? `${modulesNotOk.length} module${modulesNotOk.length === 1 ? '' : 's'} ${modulesNotOk.every((m) => m.status === 'skipped') ? 'skipped' : 'to look at'}` : 'install, identity, what it sent'}
        warn={modulesNotOk.some((m) => m.status === 'error')}
      >
        {/* Only what is not said above: the access level, the heartbeat, the clock and the modules that are fine are already in the chips,
            the tier ladder and the connection, so they come back here only for an agent that has no report to say them. */}
        <div className="space-y-3" data-testid="agent-technical-body">
          <Facts
            rows={[
              ['Install', reported ? [a.namespace, a.releaseName].filter(Boolean).join(' · ') || 'not reported by this agent yet' : undefined],
              ['Identity', a.fingerprint ? <span key="fp" className="font-mono text-xs" title={a.fingerprint}>{a.fingerprint.replace(/^[^:]*:/, '').slice(0, 8)}…</span> : undefined],
              ['Requested', a.requestedAt ? when(a.requestedAt) : undefined],
              [
                'Sent',
                l ? (
                  <span key="sent" title="Since the server started. Sizes are of the messages as encoded, before gRPC framing and TLS.">
                    {[bytesTotal(l.bytes), `${num(l.syncs)} cluster reports`, `${num(l.flows)} traffic reports`, l.measurements > 0 ? `${num(l.measurements)} path measurements` : undefined, `${l.connects} connection${l.connects === 1 ? '' : 's'}`].filter(Boolean).join(' · ')}
                  </span>
                ) : (
                  'nothing counted yet'
                ),
              ],
              [
                'Access',
                d ? undefined : <span key="access" title="Approved access is what actually runs. It can never be approved above the enrollment ceiling set when the agent joined, and stays capped at whatever the cluster's installed RBAC allows until that install is upgraded to match.">{accessWords}</span>,
              ],
              ['Last heartbeat', d ? undefined : a.lastHeartbeat ? `${when(a.lastHeartbeat)} (${ageOf(a.lastHeartbeat)})` : undefined],
            ]}
          />
          {modulesNotOk.length > 0 && (
            <ul className="space-y-1" data-testid="agent-modules-not-ok">
              {modulesNotOk.map((m) => (
                <li key={m.name} className="text-xs">
                  <span className={clsx('font-medium', MODULE_STYLE[m.status])}>{m.name} ({m.status})</span>
                  {m.reason && <span className="text-nb-500"> · {m.reason}</span>}
                </li>
              ))}
            </ul>
          )}
          <div>
            {!d && <ScopeLine agent={a} />}
            <ObserverLine agent={a} />
            <CheckLine agent={a} />
          </div>
        </div>
      </Fold>
      <Fold title="History" testId="agent-history">
        <EntityHistory kind="agent" id={a.id} />
      </Fold>
      {onRevoke && (
        <div>
          <Button size="sm" variant="danger" onClick={onRevoke} aria-label={`Revoke ${a.name}`}>Revoke…</Button>
        </div>
      )}
    </div>
  )
}

/** A closed section: a title with what is inside it in a few words, opened on a click. */
function Fold({ title, hint, warn, testId, children }: { title: string; hint?: string; warn?: boolean; testId?: string; children: ReactNode }) {
  return (
    <details className="group" data-testid={testId}>
      <summary className="flex cursor-pointer list-none items-baseline gap-1.5 text-sm text-nb-400 hover:text-nb-300 [&::-webkit-details-marker]:hidden">
        <ChevronRight size={14} className="shrink-0 self-center transition-transform group-open:rotate-90" aria-hidden />
        {title}
        {hint && <span className={clsx('text-xs', warn ? 'text-warn' : 'text-nb-600')}>{hint}</span>}
      </summary>
      <div className="mt-3 pl-5">{children}</div>
    </details>
  )
}

const Heading = ({ children }: { children: string }) => <SectionLabel className="mb-1.5">{children}</SectionLabel>

function Facts({ rows }: { rows: [ReactNode, ReactNode][] }) {
  return (
    <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-1">
      {rows.filter((r) => !!r[1]).map(([k, v], i) => (
        <Fragment key={i}>
          <dt className="text-nb-500">{k}</dt>
          <dd className="min-w-0 break-words text-nb-300">{v}</dd>
        </Fragment>
      ))}
    </dl>
  )
}
