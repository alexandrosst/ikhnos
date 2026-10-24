import { CopyCommand } from '@/components/ui/CopyCommand'
import type { Agent } from '@/lib/types'

/**
 * What is left in the cluster after an agent was revoked or rejected, and what to run about it. For an agent that a controller keeps
 * the answer is that the controller removes it; the commands are then only for an agent that is still there after a while.
 */
export function AgentTeardown({ status, teardown }: { status: 'revoked' | 'rejected'; teardown: NonNullable<Agent['teardown']> }) {
  return (
    <div className="md:col-span-2 xl:col-span-3" data-testid="agent-teardown">
      <div className="mb-1.5 text-xs font-medium uppercase tracking-wide text-nb-500">Clean up in the cluster</div>
      {teardown.keptByController && status === 'revoked' ? (
        <p className="max-w-2xl text-xs leading-relaxed text-nb-500" data-testid="agent-teardown-controller">
          A controller keeps this agent, and removes it now: the agent, its permissions and its identity. There is nothing to run. To connect this cluster again, create a new token and run the install command with it.
          {' '}If it is still in the cluster after a few minutes (the controller is not running, or it is stuck), these remove it by hand:
        </p>
      ) : (
        <p className="max-w-2xl text-xs leading-relaxed text-nb-500">
          {status === 'revoked' ? 'Revoking closed the connection and stopped this agent from being trusted, but it did not remove anything from the cluster.' : 'Rejecting stopped this enrollment, but nothing already installed in the cluster was removed.'}
          {' '}The ServiceAccount, its RBAC and the running pods stay until you remove them there:
          {teardown.namespaceGuessed && <span className="text-warn"> this agent never reported enough about its install to be sure — the commands below assume namespace <code>ikhnos-system</code> and release <code>ikhnos-agent</code>, so check both are right before running them.</span>}
        </p>
      )}
      <div className="mt-2 space-y-2">
        <CopyCommand text={teardown.helm} />
        <p className="text-xs text-nb-600">The identity Secret survives an uninstall unless this agent was installed with &ldquo;Delete the agent&apos;s identity when it is uninstalled&rdquo;, so a mistaken uninstall can&apos;t orphan re-enrollment. If it was installed with that option, the uninstall above removes the Secret too and there is nothing more to do. If not (or you are not sure), delete it as well when you want a clean slate before installing fresh here:</p>
        <CopyCommand text={teardown.secret} />
      </div>
    </div>
  )
}
