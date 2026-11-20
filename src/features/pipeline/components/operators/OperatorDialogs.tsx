import { ConfirmModal } from '@/components/forms'
import type { useFusion } from '@/features/fusion/components/useFusion'
import { CENTRAL_OPERATOR_ID, type fusionSentence } from '@/features/fusion/lib/fusionStatus'
import { isReportingHealth, receiverAuthOf } from '@/features/pipeline/lib/operatorHealth'
import type { ComponentRow, Todo } from '@/features/pipeline/lib/operatorsView'
import type { CreatedOperator } from '@/lib/api'
import type { Cluster, RegionalOperator } from '@/lib/types'
import CreateOperatorModal from './CreateOperatorModal'
import HealthModal from './HealthModal'
import { OperatorAddressModal } from './OperatorAddress'
import OperatorCertificatesModal from './OperatorCertificatesModal'
import OperatorCreated from './OperatorCreated'
import OperatorSendersModal from './OperatorSendersModal'
import RemoveOperatorModal from './RemoveOperatorModal'
import WhatToDoModal from './WhatToDoModal'

/** What "Renew certificates" replaces, said before it is done: the server issues new certificates and, for an operator that has them, a new
 *  receiver token and health credential, and none of it reaches the running operator by itself. */
function renewWarning(op: RegionalOperator): string {
  const replaced = ['its certificates']
  if (receiverAuthOf(op) === 'bearer') replaced.push('its receiver token')
  if (isReportingHealth(op)) replaced.push('its health credential (the heartbeat secret)')
  const list = replaced.length > 1 ? `${replaced.slice(0, -1).join(', ')} and ${replaced[replaced.length - 1]}` : replaced[0]
  return `This replaces ${list}. Nothing reaches the running operator by itself: the current ones stop being the current ones once its release restarts, so ${op.name} must be updated with the new commands that follow, run where it is installed${isReportingHealth(op) ? ' (until then it shows as offline)' : ''}. They are shown once.`
}

/** The dialogs the Pipeline page can have open, one at a time, with what each is about. */
export type PipelineDialog =
  | { kind: 'create' }
  | { kind: 'todo'; row: ComponentRow }
  | { kind: 'confirm-renew'; operator: RegionalOperator }
  | { kind: 'renewed'; result: CreatedOperator; operator: RegionalOperator }
  | { kind: 'remove'; operator: RegionalOperator; mode: 'revoke' | 'delete' }
  | { kind: 'health'; operator: RegionalOperator }
  | { kind: 'address'; operator: RegionalOperator }
  | { kind: 'certs'; operator: RegionalOperator }
  | { kind: 'senders'; operator: RegionalOperator }

/** What the dialogs need from the page that owns them. */
export interface OperatorDialogsProps {
  dialog: PipelineDialog | null
  onClose: () => void
  operators: RegionalOperator[]
  clusters: Cluster[]
  fusion: ReturnType<typeof useFusion>
  fusionKind: ReturnType<typeof fusionSentence>['kind']
  canConsent: boolean
  /** Whether a renewal is running (they are one at a time). */
  renewing: boolean
  onRenew: (op: RegionalOperator) => void
  onTodo: (kind: NonNullable<Todo['action']>['kind']) => void
  /** Start connecting an agent's cluster to an operator. */
  onConnect: (agentId: string, operatorId: string) => void
  /** An action finished: clear the page's error and read the operators again. */
  onDone: () => void
  /** The list of operators changed (one was created). */
  onCreated: () => void
}

/** The page's dialogs, chosen by `dialog.kind`: nothing is rendered for a dialog that is not the one open. */
export default function OperatorDialogs(p: OperatorDialogsProps) {
  const { dialog: d, onClose } = p
  if (!d) return null
  switch (d.kind) {
    case 'todo':
      return d.row.verdict.todo ? <WhatToDoModal row={d.row} todo={d.row.verdict.todo} onClose={onClose} onAction={p.onTodo} busy={p.renewing && d.row.verdict.todo.action?.kind === 'renew' ? 'Another renewal is in progress' : undefined} /> : null
    case 'create':
      return <CreateOperatorModal operators={p.operators} fusion={p.fusion} onCreated={p.onCreated} onConnect={p.onConnect} onClose={onClose} />
    case 'confirm-renew':
      return <ConfirmModal title={`Renew certificates for ${d.operator.name}?`} message={renewWarning(d.operator)} confirmLabel="Renew" onConfirm={() => p.onRenew(d.operator)} onClose={onClose} />
    case 'renewed':
      return (
        <OperatorCreated
          created={d.result}
          mode="reinstalled"
          exposure={d.operator.exposure ?? 'cluster'}
          fusionKind={p.fusionKind}
          onConnect={p.canConsent ? (id) => p.onConnect(id, d.operator.id) : undefined}
          onClose={onClose}
        />
      )
    case 'senders':
      return <OperatorSendersModal operator={d.operator} onClose={onClose} />
    case 'certs':
      return <OperatorCertificatesModal operator={d.operator} clusterName={(id) => p.clusters.find((c) => c.id === id)?.name ?? ''} onClose={onClose} />
    case 'address':
      return (
        <OperatorAddressModal
          operator={d.operator}
          central={d.operator.id === CENTRAL_OPERATOR_ID ? { service: p.fusion.status?.central?.service ?? '', namespace: p.fusion.status?.central?.namespace ?? '' } : undefined}
          onClose={onClose}
          onDone={() => { p.onDone(); void p.fusion.refresh() }}
        />
      )
    case 'health':
      return <HealthModal operator={d.operator} onClose={onClose} onDone={p.onDone} />
    case 'remove':
      return <RemoveOperatorModal operator={d.operator} mode={d.mode} onClose={onClose} onDone={p.onDone} />
  }
}
