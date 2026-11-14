import type { ReactNode } from 'react'

/**
 * What a regional operator can touch, as facts. One component for the two places that say it: the fold on the Pipeline page (what is true of
 * every operator, so `heartbeat` and `receiver` are left out) and the screen shown right after one is created (what is true of this one).
 */
export default function OperatorAccess({ heartbeat, receiver, configure = true }: { heartbeat?: boolean; receiver?: 'mtls' | 'token'; configure?: boolean }) {
  const server =
    heartbeat === undefined ? 'Only if health reporting is on' : heartbeat ? 'A health check, once a minute. No telemetry, no cluster data.' : 'Never. Health reporting is off.'
  const auth =
    receiver === 'mtls'
      ? 'A client certificate from its own authority. No token.'
      : receiver === 'token'
        ? 'A receiver token, shown once, on top of mutual TLS.'
        : 'A client certificate from its own authority. An older operator also has a token.'
  return (
    <dl className="divide-y divide-nb-850 text-sm" data-testid="operator-access">
      <Item label="Kubernetes API" testId="operator-no-rbac-note">None. No roles, and no token mounted.</Item>
      <Item label="Reaches this server" testId="operator-access-server">{server}</Item>
      <Item label="Agents authenticate with" testId="operator-access-receiver">{auth}</Item>
      {configure && <Item label="You configure">Where it sends, and health reporting (optional).</Item>}
    </dl>
  )
}

function Item({ label, children, testId }: { label: string; children: ReactNode; testId?: string }) {
  return (
    <div className="grid grid-cols-[10.5rem_1fr] gap-3 px-4 py-2.5" data-testid={testId}>
      <dt className="text-nb-300">{label}</dt>
      <dd className="min-w-0 text-nb-400">{children}</dd>
    </div>
  )
}
