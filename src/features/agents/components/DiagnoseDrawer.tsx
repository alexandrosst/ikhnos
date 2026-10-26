import { ChevronRight, ExternalLink, Stethoscope } from 'lucide-react'
import { useMemo, useState } from 'react'
import { CopyCommand } from '@/components/ui/CopyCommand'
import { StatusBadge, StatusPip, type StatusTone } from '@/components/ui/Status'
import { Button, CopyButton, Modal, ICON_SM } from '@/components/ui/primitives'
import { OpenError, useOpenPage } from '@/features/fusion/components/OpenPages'
import { useFusion } from '@/features/fusion/components/useFusion'
import { diagnoseAgent, diagnosisText, oneLine, type Check, type Diagnosis, type Finding } from '@/lib/diagnose'
import type { AgentExtras } from '@/lib/consent'
import type { Agent } from '@/lib/types'
import { useServer } from '@/store/server'

const GROUPS: Check['group'][] = ['Connection', 'Server and agent agree', 'Certificate and clock', 'What it can see', 'What it reports']

/** The look of a verdict: the same six tones every status in the product has. */
const VERDICT_CARD: Record<Diagnosis['verdict'], string> = {
  healthy: 'border-ok/30 bg-ok/5',
  attention: 'border-warn/30 bg-warn/5',
  down: 'border-bad/30 bg-bad/5',
  setup: 'border-info/30 bg-info/5',
  unknown: 'border-nb-850 bg-nb-925',
}

/**
 * Diagnose: one place that says what is wrong with an agent and in what order to deal with it, from what the server already holds (its
 * stream, its conditions, its certificate, what it reports about itself). It does not reach into the cluster. Every line names what was
 * looked at, fine or not, so "nothing is wrong" is something that was checked.
 */
export function DiagnoseDrawer({ agent, extras, now, onClose }: { agent: Agent; extras: AgentExtras; now: number; onClose: () => void }) {
  const dx = useMemo(() => diagnoseAgent(agent, extras, now), [agent, extras, now])
  const isAdmin = useServer((s) => s.isAdmin())
  const refresh = useServer((s) => s.refresh)
  const fusion = useFusion(isAdmin)
  const page = useOpenPage()
  const grafana = !!fusion.status?.links?.grafana && !!agent.clusterId
  const [showChecks, setShowChecks] = useState(false)
  const count = countChecks(dx.checks)
  const n = dx.findings.length
  // One sentence at the top, the one a person reads first: how many things to look at, or that there is nothing to do.
  const lead = n > 0 ? `${n} ${n === 1 ? 'thing' : 'things'} to look at` : dx.verdict === 'healthy' ? 'Nothing to do' : dx.headline
  const sub = n > 0 ? undefined : dx.verdict === 'healthy' ? `${dx.checks.length} checks, all fine.` : `${dx.checks.length} checks made.`
  return (
    <Modal open side onClose={onClose} title={`Diagnose ${agent.name}`} description="Worst first. It reads what the server holds; it does not reach into the cluster." width="max-w-xl">
      <div className="space-y-5" data-testid="diagnose">
        <section className={`rise-in rounded-xl border px-5 py-4 ${VERDICT_CARD[dx.verdict]}`} style={{ '--i': 0 } as React.CSSProperties} data-testid="diagnose-verdict" data-verdict={dx.verdict}>
          <div className="flex flex-wrap items-center gap-2">
            <StatusBadge tone={dx.verdict} />
            <span className="text-base font-medium text-nb-200" data-testid="diagnose-lead">{lead}</span>
          </div>
          {sub && <p className="mt-1 text-sm text-nb-400">{sub}</p>}
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <CopyButton text={diagnosisText(agent, extras, dx, now)} label="Copy summary" />
            <Button size="sm" onClick={() => void refresh()}>Check again</Button>
            {grafana && (
              <Button size="sm" disabled={page.opening} onClick={() => void page.open('grafana', { dashboard: 'ikhnos-self', cluster: agent.clusterId })} data-testid="diagnose-grafana">
                <ExternalLink size={ICON_SM} aria-hidden /> Its charts in Grafana
              </Button>
            )}
          </div>
          <OpenError message={page.error} />
        </section>

        {n > 0 && (
          <ol className="space-y-2" data-testid="diagnose-findings" aria-label="What to look at">
            {dx.findings.map((f, i) => <FindingCard key={f.id} finding={f} index={i} />)}
          </ol>
        )}

        <section aria-labelledby="dx-checked">
          <button type="button" id="dx-checked" className="flex w-full flex-wrap items-center gap-x-3 gap-y-2 rounded-xl border border-nb-850 bg-nb-925 px-4 py-2.5 text-left text-sm hover:bg-nb-930" aria-expanded={showChecks} onClick={() => setShowChecks((v) => !v)} data-testid="diagnose-checked-toggle">
            <ChevronRight size={ICON_SM} aria-hidden className={`shrink-0 text-nb-500 transition-transform ${showChecks ? 'rotate-90' : ''}`} />
            <span className="text-nb-300">Checked: {count.text}</span>
            <span className="flex flex-wrap items-center gap-1" aria-hidden>
              {dx.checks.map((c) => <span key={c.id} className="inline-flex" title={`${c.label}: ${c.value}`}><StatusPip tone={c.tone} /></span>)}
            </span>
          </button>
          {showChecks && (
            <div className="mt-3 space-y-2">
              {GROUPS.map((g) => {
                const rows = dx.checks.filter((c) => c.group === g)
                if (rows.length === 0) return null
                const look = rows.filter((c) => c.tone !== 'healthy').length
                return (
                  <details key={g} open={look > 0} className="group rounded-xl border border-nb-850 bg-nb-925" data-testid="diagnose-group" data-group={g}>
                    <summary className="flex cursor-pointer list-none items-center gap-2 px-4 py-2 text-xs font-medium uppercase tracking-wide text-nb-500">
                      <ChevronRight size={ICON_SM} aria-hidden className="shrink-0 transition-transform group-open:rotate-90" />
                      <span>{g}</span>
                      <span className="ml-auto font-normal normal-case tracking-normal">{look > 0 ? `${look} to look at` : `${rows.length} fine`}</span>
                    </summary>
                    <ul className="divide-y divide-nb-850/60 border-t border-nb-850/60">
                      {rows.map((c) => (
                        <li key={c.id} className="px-4 py-2.5 text-sm" data-testid="diagnose-check" data-check={c.id} data-tone={c.tone}>
                          {/* The dot, the name and the value share one line and one centre; a note, when there is one, sits under the name. */}
                          <div className="flex items-center gap-3">
                            <StatusPip tone={c.tone} />
                            <span className="min-w-0 flex-1 text-nb-300">{c.label}</span>
                            <span className="max-w-[55%] break-words text-right text-nb-400">{c.value}</span>
                          </div>
                          {c.note && <p className="mt-0.5 break-words pl-[1.125rem] text-xs text-nb-500">{c.note}</p>}
                        </li>
                      ))}
                    </ul>
                  </details>
                )
              })}
            </div>
          )}
        </section>
      </div>
    </Modal>
  )
}

function countChecks(checks: Check[]): { text: string } {
  const fine = checks.filter((c) => c.tone === 'healthy').length
  const unknown = checks.filter((c) => c.tone === 'unknown').length
  const look = checks.length - fine - unknown
  if (look === 0 && unknown === 0) return { text: `${fine} fine` }
  return { text: [`${fine} fine`, look > 0 ? `${look} to look at` : '', unknown > 0 ? `${unknown} not known` : ''].filter(Boolean).join(', ') }
}

/** A finding, small: what it is, the first thing to do, and the command to paste when there is one. The rest opens on a click. */
function FindingCard({ finding: f, index }: { finding: Finding; index: number }) {
  const [open, setOpen] = useState(false)
  const line = oneLine(f)
  const more = open || undefined
  const hasMore = f.detail !== line || (f.fix !== undefined && f.fix !== line) || !!f.command
  return (
    <li className="rise-in rounded-xl border border-nb-850 bg-nb-925" style={{ '--i': index + 1 } as React.CSSProperties} data-testid="diagnose-finding" data-tone={f.tone} data-finding={f.id} data-open={more}>
      <div className="flex items-start gap-2 px-4 py-3">
        <button type="button" className="min-w-0 flex-1 text-left" aria-expanded={hasMore ? open : undefined} disabled={!hasMore} onClick={() => setOpen((v) => !v)} data-testid="diagnose-finding-toggle">
          <span className="flex flex-wrap items-center gap-2">
            <span className="text-xs tabular-nums text-nb-600">{index + 1}</span>
            <StatusBadge tone={f.tone} />
            <span className="text-sm font-medium text-nb-300">{f.title}</span>
          </span>
          <span className="mt-1 block break-words text-sm text-nb-400">{line}</span>
          {hasMore && <span className="mt-1 inline-flex items-center gap-1 text-xs text-nb-500"><ChevronRight size={ICON_SM} aria-hidden className={`transition-transform ${open ? 'rotate-90' : ''}`} />{open ? 'Less' : 'More'}</span>}
        </button>
        {f.command && !open && <CopyButton text={f.command} label="Copy command" />}
      </div>
      {open && (
        <div className="space-y-1.5 border-t border-nb-850/60 px-4 py-3">
          {f.detail !== line && <p className="break-words text-sm text-nb-400">{f.detail}</p>}
          {f.fix && f.fix !== line && <p className="text-sm text-nb-300"><span className="text-nb-500">What to do: </span>{f.fix}</p>}
          {f.command && <CopyCommand text={f.command} multiline label={`Copy the command for: ${f.title}`} testId="diagnose-command" />}
        </div>
      )}
    </li>
  )
}

/** The button that opens it. A small, plain control: it sits in a row beside the agent’s other actions. */
export function DiagnoseButton({ onClick, tone }: { onClick: () => void; tone?: StatusTone }) {
  return (
    <Button size="sm" onClick={(e) => { e.stopPropagation(); onClick() }} data-testid="diagnose-open" title="What is wrong with this agent, and what to do about it">
      <Stethoscope size={ICON_SM} aria-hidden /> Diagnose {tone && tone !== 'healthy' && tone !== 'unknown' && <StatusPip tone={tone} pulse={tone === 'down'} />}
    </Button>
  )
}
