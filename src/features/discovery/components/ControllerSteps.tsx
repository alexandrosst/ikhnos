import { CopyButton } from '@/components/ui/primitives'
import type { ControllerDoc } from '@/lib/generated/wire'

/**
 * The same install as the helm command, through the optional controller: one command. It installs the controller (or updates one that is
 * there), hands it the one-time token and asks it for the agent, so there is nothing to create first. The server writes it in the call that
 * issues the token, from the same choices as the helm command, so the two views cannot say different things.
 */
export default function ControllerSteps({ c }: { c: ControllerDoc }) {
  if (!c.available) {
    return (
      <div className="rounded-md border border-nb-850 bg-nb-930 px-3 py-2.5 text-xs text-nb-400" data-testid="controller-unavailable">
        <div className="mb-1 font-medium text-nb-200">The controller is not offered here</div>
        <p>{c.reason}</p>
        <p className="mt-1.5">The helm command in the other tab installs the same agent.</p>
      </div>
    )
  }
  return (
    <div className="space-y-4" data-testid="controller-steps">
      <div>
        <div className="mb-1 flex items-center justify-between text-sm text-nb-300">
          <span>
            Run this where you use <code className="font-mono text-xs">helm</code> for the cluster
          </span>
          <CopyButton text={c.install} label="Copy command" />
        </div>
        <pre className="whitespace-pre-wrap break-all rounded-md border border-nb-850 bg-nb-950 px-3 py-3 font-mono text-xs leading-relaxed text-nb-300" data-testid="controller-install">{c.install}</pre>
        <p className="mt-1.5 text-xs text-nb-500">
          That is all there is to do. It installs the controller in <code className="font-mono">{c.namespace}</code> with permission for access tier {c.maxTier} and no more,
          and asks it for this agent with the one-time token. Deleting the <code className="font-mono">IkhnosAgent</code> resource it creates uninstalls the agent.
        </p>
      </div>
      {c.raise && (
        <div>
          <div className="mb-1 flex items-center justify-between text-xs text-nb-400">
            <span>A controller that is already installed with a lower ceiling than tier {c.maxTier} refuses the agent and says so in its status. Raise it with:</span>
            <CopyButton text={c.raise} label="Copy command" />
          </div>
          <pre className="whitespace-pre-wrap break-all rounded-md border border-nb-850 bg-nb-950 px-3 py-3 font-mono text-xs leading-relaxed text-nb-300" data-testid="controller-raise">{c.raise}</pre>
        </div>
      )}
    </div>
  )
}
