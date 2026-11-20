import { DataBoundary } from '@/components/ui/DataBoundary'
import { FusionSection, ROWS } from '@/features/fusion/components/FusionSection'
import { ErrorBanner, Pill, SkeletonBlock } from '@/components/ui/primitives'
import { api, type FusionApplications, type FusionServices, type FusionSignal } from '@/lib/api'
import { useLoad } from '@/lib/useLoad'

const SIGNAL_WORD: Record<FusionSignal, string> = { metrics: 'Metrics', logs: 'Logs', traces: 'Traces' }
const SHOWN_SERVICES = 25

/** The signals FUSION holds for something, or that it has none yet. */
function Signals({ list }: { list: FusionSignal[] }) {
  if (list.length === 0) return <span className="text-xs text-nb-500">No data yet</span>
  return <span className="inline-flex flex-wrap gap-1">{list.map((s) => <Pill key={s}>{SIGNAL_WORD[s]}</Pill>)}</span>
}

/** What FUSION holds: for each ikhnos application the services in it and which signals arrived for them over the last day, and the services
 *  with data that belong to none (an exporter, a meter). Read from the same data API an API token reads. */
export function FusionData() {
  const load = useLoad(
    async (c) => {
      const [apps, services] = await Promise.all([api.getFusionApplications(c), api.getFusionServices(c)])
      return { apps, services }
    },
    { failure: 'Could not read what FUSION holds.' },
  )
  return <DataBoundary load={load} loading={<SkeletonBlock className="h-40 w-full" />}>{({ apps, services }) => <FusionDataBody apps={apps} services={services} />}</DataBoundary>
}

function FusionDataBody({ apps, services }: { apps: FusionApplications; services: FusionServices }) {
  const warnings = [...new Set([...apps.warnings, ...services.warnings])]
  const loose = services.services.filter((s) => s.applications.length === 0)
  return (
    <>
      {warnings.map((w) => <ErrorBanner key={w} className="mb-4">{w}</ErrorBanner>)}
      <FusionSection title="Applications" description="Signals FUSION received for each application in the last 24 hours." testId="fusion-applications">
        <ul className={ROWS}>
          {apps.applications.map((a) => (
            <li key={a.id} className="flex flex-wrap items-center gap-x-4 gap-y-1.5 px-4 py-3" data-testid={`fusion-app-${a.name}`}>
              <div className="min-w-0 sm:w-56">
                <div className="truncate text-nb-300" title={a.name}>{a.name}</div>
                <div className="mt-0.5 text-xs text-nb-500">
                  {a.services.length} {a.services.length === 1 ? 'service' : 'services'}{a.clusters.length > 0 && <> in {a.clusters.join(', ')}</>}
                  {a.unresolvedMembers ? <>, {a.unresolvedMembers} not running</> : null}
                </div>
              </div>
              <div className="sm:ml-auto"><Signals list={a.signals} /></div>
            </li>
          ))}
          {apps.applications.length === 0 && <li className="px-4 py-3 text-nb-500">No application has been declared in ikhnos yet.</li>}
        </ul>
      </FusionSection>

      {loose.length > 0 && (
        <FusionSection title="Other services" description="Services with data that are in no application." testId="fusion-services">
          <ul className={ROWS}>
            {loose.slice(0, SHOWN_SERVICES).map((s) => (
              <li key={s.name} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5 px-4 py-3">
                <span className="min-w-0 truncate text-nb-300" title={s.name}>{s.name}</span>
                <Signals list={s.signals} />
              </li>
            ))}
          </ul>
          {loose.length > SHOWN_SERVICES && <p className="mt-2 text-xs text-nb-500">and {loose.length - SHOWN_SERVICES} more, which the data API lists.</p>}
        </FusionSection>
      )}

      <p className="max-w-3xl text-xs leading-relaxed text-nb-500">
        Telemetry is sorted into three categories, which the data API takes as a filter: <span className="text-nb-400">system</span> (hosts and the pipeline itself),{' '}
        <span className="text-nb-400">kubernetes</span> (the cluster&apos;s own objects) and <span className="text-nb-400">application</span> (what an application reports about itself).
      </p>
    </>
  )
}
