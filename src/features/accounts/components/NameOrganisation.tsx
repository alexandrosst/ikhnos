import { useEffect, useRef, useState } from 'react'
import { Button, ErrorBanner, Input, Modal } from '@/components/ui/primitives'
import { ApiError } from '@/lib/api'
import { useServer } from '@/store/server'

/** What a fresh server calls the organisation it makes with the first account. */
const FIRST_NAME = 'Default'
const askedKey = (orgId: string) => `ikhnos:org-name-asked:${orgId}`
const wasAsked = (orgId: string) => {
  try { return localStorage.getItem(askedKey(orgId)) === '1' } catch { return false }
}

/**
 * Asks the owner, once, to name an organisation that still has the name a fresh server gave it. It comes after sign-in (which has already asked
 * for a new password) rather than in the sign-in form, because a name is not a credential. "Later" is remembered in this browser and the name
 * stays renamable on the Members page; so does saving.
 */
export default function NameOrganisation() {
  const orgId = useServer((s) => s.orgId)
  const role = useServer((s) => s.role)
  const connected = useServer((s) => s.status === 'connected')
  const name = useServer((s) => s.orgs.find((o) => o.id === s.orgId)?.name)
  const renameOrg = useServer((s) => s.renameOrg)
  const [draft, setDraft] = useState(FIRST_NAME)
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  // Closing hides it for the rest of this visit whatever happens to storage; the stored flag is for the next one.
  const [closedFor, setClosedFor] = useState<string>()
  const input = useRef<HTMLInputElement>(null)

  const show = connected && !!orgId && role === 'owner' && name === FIRST_NAME && closedFor !== orgId && !wasAsked(orgId)
  useEffect(() => {
    if (show) input.current?.select()
  }, [show])
  if (!show || !orgId) return null

  const close = () => {
    try { localStorage.setItem(askedKey(orgId), '1') } catch { /* asked again next time: harmless */ }
    setClosedFor(orgId)
  }
  const save = async (e: React.FormEvent) => {
    e.preventDefault()
    const next = draft.trim()
    if (!next || busy) return
    if (next === FIRST_NAME) return close()
    setBusy(true)
    setError('')
    try {
      await renameOrg(next)
      close()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : 'Could not save the name.')
    }
    setBusy(false)
  }

  return (
    <Modal
      open
      onClose={close}
      title="Name your organisation"
      description="People you invite will see it."
      width="max-w-sm"
      footer={
        <>
          <Button variant="ghost" onClick={close} data-testid="org-name-later">Later</Button>
          <Button variant="primary" type="submit" form="org-name-form" disabled={!draft.trim() || busy} data-testid="org-name-save">{busy ? 'Saving…' : 'Save'}</Button>
        </>
      }
    >
      <form id="org-name-form" onSubmit={save} data-testid="org-name-dialog">
        <Input ref={input} value={draft} onChange={(e) => setDraft(e.target.value)} maxLength={80} aria-label="Organisation name" autoFocus data-testid="org-name-input" />
        {error && <ErrorBanner className="mt-3">{error}</ErrorBanner>}
      </form>
    </Modal>
  )
}
