import { Loader2 } from 'lucide-react'
import { useState } from 'react'
import { Button, ICON_SM } from '@/components/ui/primitives'
import { api, ApiError } from '@/lib/api'
import type { TelemetryRequestDoc } from '@/lib/generated/wire'
import type { TelemetryInput } from '@/lib/install'
import { notInARequest, requestChange, requestOf, sameAsRequest, shareProblem, widens, type AskAvailability } from '@/lib/telemetryRequest'
import { useConn } from '@/store/server'

/**
 * The third way to make a change, next to the helm command and the patch of the resource: the server asks the cluster, and the cluster's
 * controller decides. It carries the signals and the share of traces, nothing else, so anything else the form changes is listed as left
 * out (the command carries it). It never blocks on a request still being handled: sending again replaces it with a new revision, and a
 * request nothing answers (an agent that is off) can be taken back.
 */
export function AskTheCluster({
  agentId,
  availability,
  draft,
  installed,
  installedShare,
  changes,
  request,
  isAdmin,
  onAnswer,
  testId,
}: {
  agentId: string | undefined
  availability: AskAvailability
  draft: TelemetryInput
  installed: readonly string[]
  installedShare: number | undefined
  /** The review's lines (describeTelemetryChanges): the ones a request does not carry are listed. */
  changes: readonly string[]
  request: TelemetryRequestDoc | undefined
  isAdmin: boolean
  /** The server's answer to what was just sent, so the line under the form moves before the next poll. */
  onAnswer: (doc: TelemetryRequestDoc) => void
  testId: string
}) {
  const conn = useConn()
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')

  if (availability === 'old') {
    return <p className="text-nb-400" data-testid={`${testId}-old`}>This agent is too old to take a request from the server. Upgrade it to ask; until then the cluster keeps the settings it has, and the commands still work.</p>
  }
  if (availability === 'none') return null

  const ask = requestOf(draft)
  const change = requestChange(installed, installedShare, draft)
  const differs = change.on.length + change.off.length > 0 || change.share
  const already = sameAsRequest(request, draft)
  const left = notInARequest(changes)
  const problem = shareProblem(draft)
  const needsAdmin = !isAdmin && widens(request, ask)
  const blocked = problem ?? (needsAdmin ? 'Asking the cluster to collect more takes an administrator; asking for less does not.' : already ? 'This is what the cluster was already asked. Change it to ask again, or take the request back.' : !differs ? 'Nothing to change from what the cluster collects now.' : undefined)

  const run = async (body: { signals?: typeof ask.signals; tracesSamplingPercent?: number }) => {
    if (!agentId || sending) return
    setSending(true)
    setError('')
    try {
      onAnswer(await api.setAgentTelemetry(conn, agentId, body))
    } catch (e) {
      setError(e instanceof ApiError || e instanceof Error ? e.message : 'The request could not be sent.')
    } finally {
      setSending(false)
    }
  }

  return (
    <div className="space-y-2" data-testid={testId}>
      <p>
        The server sends this to the cluster. Its controller applies it, within what its owner allowed, and puts it back if the agent does not come up. Where the data goes is not part of a
        request: the destination, credentials, scopes and tags stay as the cluster&apos;s owner set them.
      </p>
      {left.length > 0 && (
        <div className="rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-warn" data-testid={`${testId}-left-out`}>
          <p className="font-medium">Not part of a request, so left as they are:</p>
          <ul className="mt-0.5 list-disc pl-5 text-nb-300">
            {left.map((l) => (
              <li key={l}>{l}</li>
            ))}
          </ul>
          <p className="mt-0.5 text-nb-400">Use the command for those.</p>
        </div>
      )}
      {blocked && (
        <p className="text-xs text-nb-500" role="status" data-testid={`${testId}-blocked`}>
          {blocked}
        </p>
      )}
      {error && (
        <div role="alert" className="rounded-md border border-bad/30 bg-bad/10 px-3 py-2 text-bad" data-testid={`${testId}-error`}>
          {error}
        </div>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="primary" size="sm" disabled={!agentId || sending || blocked !== undefined} onClick={() => void run(ask)} data-testid={`${testId}-send`}>
          {sending ? <Loader2 size={ICON_SM} className="animate-spin" aria-hidden /> : null}
          Send request
        </Button>
        {request?.requested && (
          <Button size="sm" disabled={!agentId || sending} onClick={() => void run({})} data-testid={`${testId}-take-back`}>
            Take the request back
          </Button>
        )}
      </div>
    </div>
  )
}
