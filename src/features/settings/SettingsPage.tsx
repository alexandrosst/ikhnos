import clsx from 'clsx'
import { Database, Server, UserRound } from 'lucide-react'
import { useEffect } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { ICON_SM, PageHeader } from '@/components/ui/primitives'
import AboutSettings from '@/features/settings/components/AboutSettings'
import AccountSettings from '@/features/settings/components/AccountSettings'
import DataSettings from '@/features/settings/components/DataSettings'
import InstallationSettings from '@/features/settings/components/InstallationSettings'
import MailSettings from '@/features/settings/components/MailSettings'
import ServerAddressSettings from '@/features/settings/components/ServerAddressSettings'
import { useConn, useServer } from '@/store/server'

type Pane = 'account' | 'server' | 'data'

const PANES: { id: Pane; label: string; icon: typeof Server }[] = [
  { id: 'account', label: 'Account', icon: UserRound },
  { id: 'server', label: 'Server', icon: Server },
  { id: 'data', label: 'Data', icon: Database },
]

/** The older section addresses (the install wizard links to /settings#installation) land on the pane that holds the section. */
const PANE_OF_HASH: Record<string, Pane> = {
  account: 'account', you: 'account', 'sign-in': 'account',
  server: 'server', 'server-address': 'server', installation: 'server', email: 'server', about: 'server',
  data: 'data', danger: 'data',
}

/**
 * Three short pages in a rail rather than one long scroll: what is yours (Account), what this server is and where it sends agents (Server) and
 * your topology as a file (Data). Each is a list of rows. The address says which one is open, so a link can point at any of them.
 */
export default function SettingsPage() {
  const connected = useServer((s) => s.status === 'connected')
  const canManageMail = useServer((s) => s.status === 'connected' && !!s.user?.canManageMail)
  const conn = useConn()
  const { hash } = useLocation()
  const navigate = useNavigate()

  // Signed out there is only the topology in this browser: no rail, just that page.
  const panes = connected ? PANES : PANES.filter((p) => p.id === 'data')
  const wanted = PANE_OF_HASH[hash.slice(1)]
  const pane: Pane = panes.some((p) => p.id === wanted) ? wanted : panes[0].id

  // The page is lazy, and a pane renders after the address does: scroll to the section a link names once it is there.
  useEffect(() => {
    if (hash) document.getElementById(hash.slice(1))?.scrollIntoView({ block: 'start' })
  }, [hash, pane, connected])

  return (
    <>
      <PageHeader title="Settings" />
      <div className={clsx('grid gap-6', panes.length > 1 && 'lg:grid-cols-[10rem_minmax(0,40rem)] lg:gap-10')}>
        {panes.length > 1 && (
          <nav aria-label="Settings" className="flex gap-1 lg:sticky lg:top-0 lg:flex-col lg:self-start" data-testid="settings-rail">
            {panes.map((p) => (
              <button
                key={p.id}
                type="button"
                onClick={() => navigate({ hash: p.id }, { replace: true })}
                aria-current={p.id === pane ? 'page' : undefined}
                className={clsx(
                  'flex items-center gap-2.5 rounded-md px-3 py-1.5 text-left text-sm transition-colors',
                  p.id === pane ? 'bg-nb-930 text-nb-300' : 'text-nb-500 hover:bg-nb-930 hover:text-nb-300',
                )}
                data-testid={`settings-tab-${p.id}`}
              >
                <p.icon size={ICON_SM} aria-hidden /> {p.label}
              </button>
            ))}
          </nav>
        )}
        {/* Keyed by the pane, so moving between them replays the page's own arrival. */}
        <div key={pane} className="rise-in min-w-0 space-y-6" data-testid={`settings-pane-${pane}`}>
          {pane === 'account' && <AccountSettings />}
          {pane === 'server' && (
            <>
              <ServerAddressSettings />
              <InstallationSettings conn={conn} />
              {canManageMail && <MailSettings conn={conn} />}
              <AboutSettings />
            </>
          )}
          {pane === 'data' && <DataSettings />}
        </div>
      </div>
    </>
  )
}
