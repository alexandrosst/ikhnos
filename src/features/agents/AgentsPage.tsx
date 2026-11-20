import { List, Network, Plug } from 'lucide-react'
import { Fragment, useEffect, useMemo, useState } from 'react'
import { useLocation } from 'react-router-dom'
import { useQueryParams } from '@/lib/useQueryParams'
import { AgentDetail } from '@/features/agents/components/AgentDetail'
import { AgentMap } from '@/features/agents/components/AgentMap'
import { AgentRow } from '@/features/agents/components/AgentRow'
import { HEALTH, inWords, msUntil } from '@/features/agents/lib/agentView'
import { SortTh, useSort } from '@/components/ui/sort'
import { TabPanel, Tabs } from '@/components/ui/Tabs'
import { DiagnoseDrawer } from '@/features/agents/components/DiagnoseDrawer'
import DiscoveryPanel from '@/features/discovery/components/DiscoveryPanel'
import { usePlacementSuggestions } from '@/lib/usePlacement'
import ApprovalCard from '@/features/discovery/components/ApprovalCard'
import { useConnectFlow } from '@/features/discovery/components/ConnectFlow'
import { ConfirmModal } from '@/components/forms'
import { FilterChips } from '@/components/ui/FilterChips'
import { Button, Card, EmptyState, ErrorBanner, ICON_SM, LinkButton, PageHeader, SectionHeading, SkeletonBlock, StatTile, Table, TableSkeleton, Td, Th } from '@/components/ui/primitives'
import { api, ApiError } from '@/lib/api'
import { extrasOf } from '@/lib/consent'
import { HEALTH_ORDER, healthOf, type AgentHealth } from '@/features/agents/lib/agentHealth'
import { bytesTotal } from '@/lib/observed'
import { type Agent, type Cluster } from '@/lib/types'
import { useApprovalLocks } from '@/store/approvalLocks'
import { useServer } from '@/store/server'
import { useTopology } from '@/store/topology'
import { PAGE_ICON } from '@/lib/pageIcons'

export default function AgentsPage() {
  const { agents, clusters, sites, suggestions } = useTopology()
  const openSuggestions = suggestions.filter((x) => x.status === 'open').length + usePlacementSuggestions().suggestions.length
  const [sp, setParams] = useQueryParams()
  const tab = ((t) => (t === 'suggestions' || t === 'seen' ? t : 'agents'))(sp.get('tab'))
  const map = sp.get('view') === 'map'
  const [open, setOpen] = useState<string | null>(null)
  // `/agents?open=<id>` (from the link on a local operator's detail in the Pipeline page): that agent's row opens. The parameter is spent at
  // once, so closing the row, or a reload, does not open it again.
  const wantOpen = sp.get('open')
  const [filter, setFilter] = useState<'all' | AgentHealth>('all')
  const [revoking, setRevoking] = useState<Agent | null>(null)
  const [diagnosing, setDiagnosing] = useState<string | null>(null)
  const [error, setError] = useState('')
  // A clock that only actually changes on its own tick, not on every re-render this page has plenty of
  // reasons to do for other reasons (a filter, a search, an approval lock) - reading Date.now() directly
  // here used to change on every one of those too, which fed straight into rows' useMemo below and made
  // it recompute every render regardless of whether `agents` itself had changed, same as not memoizing at
  // all. Ticking on an interval instead keeps "ago"/health staleness honest while actually letting rows
  // skip work between ticks.
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 30_000)
    return () => clearInterval(id)
  }, [])

  // Diagnostics and overrides are in the raw state document, and only for people who may change an agent's access.
  const rawAgents = useServer((s) => s.state?.agents)
  const connected = useServer((s) => s.status === 'connected')
  // Connected, and the first state has not arrived: an empty list now would say "no cluster is connected" about something not yet looked at.
  const loading = useServer((s) => s.status === 'connected' && !s.state)
  const canConsent = useServer((s) => s.status === 'connected' && s.canEdit())
  const conn = useServer((s) => s.conn)
  const refresh = useServer((s) => s.refresh)
  const connect = useConnectFlow()
  const canAdminister = connect.canStart
  // Enrollment is decided here: a request waits for its approval code, and one rejected for too many wrong codes stays (with the reason) until dismissed.
  const locked = useApprovalLocks((s) => s.locked)
  const waiting = agents.filter((a) => a.status === 'pending' && a.requestedAt)
  const pending = [...waiting, ...agents.filter((a) => a.status === 'rejected' && locked[a.id])]
  // A link to one request (`/agents#approval-<id>`, from the getting-started list) scrolls to its card once it is on the page.
  const { hash } = useLocation()
  const target = pending.some((a) => `#approval-${a.id}` === hash) || hash === '#approvals' ? hash : ''
  useEffect(() => {
    if (!target) return
    const el = document.getElementById(target.slice(1))
    el?.scrollIntoView({ block: 'start' })
    el?.focus({ preventScroll: true })
  }, [target])

  // Taken in while rendering (React's way of adjusting state to a changed input), and only once the agent is in the list: a link followed
  // before the first state has arrived waits for it.
  const [taken, setTaken] = useState<string | null>(null)
  if (!wantOpen && taken) setTaken(null)
  if (wantOpen && wantOpen !== taken && agents.some((a) => a.id === wantOpen)) {
    setTaken(wantOpen)
    setOpen(wantOpen)
  }
  useEffect(() => {
    if (wantOpen && wantOpen === taken) setParams({ open: null })
  }, [wantOpen, taken, setParams])

  const cluster = (id?: string) => clusters.find((c) => c.id === id)
  const site = (c?: Cluster) => sites.find((s) => s.id === c?.siteId)

  const rows = useMemo(() => agents.map((a) => ({ a, h: healthOf(a, now) })), [agents, now])
  const counts = useMemo(() => {
    const c: Record<string, number> = { all: rows.length }
    for (const r of rows) c[r.h] = (c[r.h] ?? 0) + 1
    return c
  }, [rows])
  const filtered = useMemo(() => rows.filter((r) => filter === 'all' || r.h === filter), [rows, filter])
  // Worst first when sorting by connection, so that "ascending" brings what needs a look to the top.
  const sortBy = useMemo(() => ({
    agent: (r: (typeof rows)[number]) => r.a.name,
    runsIn: (r: (typeof rows)[number]) => clusters.find((c) => c.id === r.a.clusterId)?.name,
    connection: (r: (typeof rows)[number]) => HEALTH_ORDER.indexOf(r.h),
    sent: (r: (typeof rows)[number]) => r.a.link?.bytes,
    access: (r: (typeof rows)[number]) => r.a.accessTier,
    status: (r: (typeof rows)[number]) => r.a.status,
  }), [clusters])
  const { sorted: shown, sort, toggle } = useSort(filtered, sortBy)
  const sent = agents.reduce((n, a) => n + (a.link?.bytes ?? 0), 0)
  const soonest = agents.filter((a) => a.status === 'approved' && a.connected !== undefined).map((a) => msUntil(a.certExpiresAt, now)).filter((d): d is number => d !== undefined).sort((x, y) => x - y)[0]
  const openAgent = agents.find((a) => a.id === open)

  const revoke = async (a: Agent) => {
    const c = conn()
    if (!c) return
    try {
      await api.revoke(c, a.id, 'revoked in the UI')
      await refresh()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : 'Could not revoke.')
    }
  }

  return (
    <>
      <PageHeader
        title="Agents"
        description="The programs running inside your clusters that report to this server: where each runs, whether it is alive, and how much it has sent."
        actions={
          <>
            {canAdminister && (
              <Button variant="primary" onClick={connect.start}>
                <Plug size={ICON_SM} /> Connect a cluster
              </Button>
            )}
          </>
        }
      />

      {/* While the wizard is open it shows the same card, so it is not repeated behind the dialog. */}
      {pending.length > 0 && !connect.wizardOpen && (
        <section id="approvals" tabIndex={-1} aria-labelledby="approvals-title" className="mb-8 scroll-mt-6 space-y-3 focus:outline-none" data-testid="approvals">
          <SectionHeading id="approvals-title">Waiting for your approval</SectionHeading>
          {canAdminister ? (
            pending.map((a) => (
              <div key={a.id} id={`approval-${a.id}`} tabIndex={-1} className="scroll-mt-6 focus:outline-none">
                <ApprovalCard agent={a} />
              </div>
            ))
          ) : (
            <Card as="p" className="px-5 py-4 text-sm text-nb-400">
              {waiting.length} cluster{waiting.length === 1 ? ' is' : 's are'} waiting. An administrator has to approve {waiting.length === 1 ? 'it' : 'them'}.
            </Card>
          )}
        </section>
      )}

      <Tabs
        label="Agents"
        idBase="agents"
        className="mb-6"
        value={tab}
        onChange={(id) => setParams({ tab: id === 'agents' ? null : id, view: null })}
        items={[
          { id: 'agents', label: 'Agents', count: agents.length || undefined, testId: 'tab-agents' },
          { id: 'suggestions', label: 'Suggestions', count: openSuggestions || undefined, testId: 'tab-suggestions', title: 'What the agents found that needs your decision' },
          { id: 'seen', label: 'Seen and gone', testId: 'tab-seen', title: 'Clusters the agents have seen but nobody connected, and records that are no longer reported' },
        ]}
      />

      <TabPanel idBase="agents" id="agents" active={tab === 'agents'}>
      {agents.length === 0 && loading ? (
        <div className="space-y-3" aria-busy><div className="grid grid-cols-2 gap-3 lg:grid-cols-4">{[0, 1, 2, 3].map((i) => <SkeletonBlock key={i} className="h-20 w-full rounded-xl" />)}</div><TableSkeleton colCount={6} rows={4} /></div>
      ) : agents.length === 0 ? (
        <EmptyState
          icon={PAGE_ICON.agents}
          title="No cluster is connected yet"
          description="Agents run inside your clusters and report to this server. Connect a cluster to install one: it dials out to this server, and nothing is read until you approve it."
          action={canAdminister ? <Button variant="primary" onClick={connect.start}><Plug size={ICON_SM} /> Connect a cluster</Button> : undefined}
        />
      ) : (
        <>
          <div className="stagger mb-6 grid grid-cols-2 gap-3 lg:grid-cols-4" data-testid="agent-stats">
            <StatTile label="Connected" value={`${counts.ok ?? 0} of ${rows.filter((r) => r.a.status === 'approved' && r.h !== 'unknown').length}`} sub={(counts.late ?? 0) + (counts.offline ?? 0) > 0 ? `${(counts.late ?? 0) + (counts.offline ?? 0)} need a look` : (counts.ok ?? 0) === 0 ? 'no agent is reporting live' : 'all approved agents are up'} tone={(counts.late ?? 0) + (counts.offline ?? 0) > 0 ? 'warn' : undefined} />
            <StatTile label="Waiting for approval" value={String(counts.waiting ?? 0)} sub={counts.waiting ? 'review them above' : 'no request is waiting'} tone={counts.waiting ? 'warn' : undefined} />
            <StatTile label="Received" value={connected ? bytesTotal(sent) : '—'} sub={connected ? 'since the server started' : 'needs a server'} />
            <StatTile label="Next certificate expiry" value={soonest === undefined ? '—' : soonest < 0 ? 'expired' : inWords(soonest)} sub={soonest === undefined ? 'no certificates yet' : 'agents renew on their own'} tone={soonest !== undefined && soonest < 0 ? 'warn' : undefined} />
          </div>

          <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
            <FilterChips
              label="Show agents"
              value={filter}
              onChange={setFilter}
              choices={(['all', 'ok', 'late', 'offline', 'waiting', 'gone', 'unknown'] as const)
                .filter((k) => k === 'all' || counts[k])
                .map((k) => ({ key: k, label: k === 'all' ? 'All' : HEALTH[k].label, count: counts[k] }))}
            />
            <Tabs
              label="How to show agents"
              variant="segmented"
              value={map}
              onChange={(id) => setParams({ view: id ? 'map' : null })}
              items={[{ id: false, label: 'List', icon: <List size={ICON_SM} aria-hidden />, testId: 'agents-list' }, { id: true, label: 'Map', icon: <Network size={ICON_SM} aria-hidden />, testId: 'agents-map' }]}
            />
          </div>

          {error && <ErrorBanner className="mb-4">{error}</ErrorBanner>}

          {map ? (
            <AgentMap rows={shown} clusterOf={cluster} selected={open} onSelect={setOpen} />
          ) : (
            <Table data-testid="agents-table">
              <thead>
                <tr>
                  <SortTh id="agent" sort={sort} onSort={toggle}>Agent</SortTh><SortTh id="runsIn" sort={sort} onSort={toggle}>Runs in</SortTh><SortTh id="connection" sort={sort} onSort={toggle}>Connection</SortTh><SortTh id="sent" sort={sort} onSort={toggle}>Sent</SortTh><SortTh id="access" sort={sort} onSort={toggle}>Access</SortTh><SortTh id="status" sort={sort} onSort={toggle}>Status</SortTh>
                  <Th className="w-28" actionsLabel="Diagnose" />
                </tr>
              </thead>
              <tbody>
                {shown.map(({ a, h }) => {
                  const c = cluster(a.clusterId)
                  const s = site(c)
                  const expanded = open === a.id
                  return (
                    <Fragment key={a.id}>
                      <AgentRow
                        agent={a}
                        health={h}
                        cluster={c}
                        site={s}
                        extras={extrasOf(rawAgents, a.id)}
                        expanded={expanded}
                        now={now}
                        onToggle={() => setOpen(expanded ? null : a.id)}
                        onDiagnose={() => setDiagnosing(a.id)}
                      />
                      {expanded && (
                        <tr>
                          <Td colSpan={7} className="bg-nb-930/40 py-4">
                            <div className="detail-in">
                              <AgentDetail agent={a} extras={extrasOf(rawAgents, a.id)} canConsent={canConsent} onRevoke={canAdminister && connected && a.status === 'approved' ? () => setRevoking(a) : undefined} />
                            </div>
                          </Td>
                        </tr>
                      )}
                    </Fragment>
                  )
                })}
                {shown.length === 0 && (
                  <tr><Td colSpan={7} className="py-8 text-center text-nb-500">No agents in this state.</Td></tr>
                )}
              </tbody>
            </Table>
          )}

          {map && openAgent && (
            <Card className="mt-4 px-5 py-4" data-testid="agent-detail">
              <div className="mb-3 flex items-center justify-between">
                <h2 className="text-sm font-medium text-nb-300">{openAgent.name}</h2>
                <LinkButton tone="quiet" className="text-xs" onClick={() => setOpen(null)}>Close</LinkButton>
              </div>
              <AgentDetail agent={openAgent} extras={extrasOf(rawAgents, openAgent.id)} canConsent={canConsent} onRevoke={canAdminister && connected && openAgent.status === 'approved' ? () => setRevoking(openAgent) : undefined} />
            </Card>
          )}
        </>
      )}
      </TabPanel>
      <TabPanel idBase="agents" id="suggestions" active={tab === 'suggestions'}>
        <DiscoveryPanel connect={connect} part="inbox" />
      </TabPanel>
      <TabPanel idBase="agents" id="seen" active={tab === 'seen'}>
        <DiscoveryPanel connect={connect} part="seen" />
      </TabPanel>

      {connect.dialogs}
      {diagnosing && agents.find((x) => x.id === diagnosing) && (
        <DiagnoseDrawer agent={agents.find((x) => x.id === diagnosing)!} extras={extrasOf(rawAgents, diagnosing)} now={now} onClose={() => setDiagnosing(null)} />
      )}

      {revoking && (
        <ConfirmModal
          title={`Revoke ${revoking.name}?`}
          message="The agent is disconnected at once and cannot come back with its current identity. Records it discovered stay in your topology. To reconnect the cluster you enroll it again with a new token. Nothing is removed from the cluster itself \u2014 its ServiceAccount, RBAC and pods stay running until you remove them there; you'll see ready-to-run cleanup commands on this agent's detail view once it's revoked."
          confirmLabel="Revoke"
          onConfirm={() => revoke(revoking)}
          onClose={() => setRevoking(null)}
        />
      )}
    </>
  )
}
