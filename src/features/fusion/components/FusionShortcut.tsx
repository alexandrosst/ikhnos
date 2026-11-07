import { ChartLine } from 'lucide-react'
import { useEffect } from 'react'
import { useLocation } from 'react-router-dom'
import clsx from 'clsx'
import { ICON_SM } from '@/components/ui/primitives'
import { useOpenPage } from '@/features/fusion/components/OpenPages'
import { useFusion } from '@/features/fusion/components/useFusion'
import { useServer } from '@/store/server'

/**
 * The small chart button on the FUSION row of the sidebar: Grafana in a new tab, without opening the FUSION page first. It is there only for
 * someone who may use FUSION and only while Grafana is up, so it never leads to a page that is not there. FUSION's state is read again whenever
 * the page changes, so what was switched on the FUSION page shows here as soon as the person leaves it.
 */
export default function FusionShortcut() {
  const admin = useServer((s) => s.isAdmin)()
  const { status, refresh } = useFusion(admin)
  const { pathname } = useLocation()
  useEffect(() => {
    void refresh()
  }, [pathname, refresh])
  const page = useOpenPage()
  if (!admin || !status?.links?.grafana) return null
  return (
    <>
      <button
        type="button"
        onClick={() => void page.open('grafana')}
        disabled={page.opening}
        title={page.error || 'Open Grafana'}
        aria-label="Open Grafana in a new tab"
        className={clsx(
          'flex size-6 items-center justify-center rounded-md border bg-nb-930 transition-colors hover:border-nb-800 hover:text-nb-300 disabled:opacity-60',
          page.error ? 'border-warn/50 text-warn' : 'border-nb-850 text-nb-500',
        )}
        data-testid="fusion-shortcut"
      >
        <ChartLine size={ICON_SM} aria-hidden />
      </button>
      {page.error && <span role="alert" className="sr-only">{page.error}</span>}
    </>
  )
}
