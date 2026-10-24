import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { AgentTeardown } from '@/features/agents/AgentTeardown'

const teardown = (keptByController: boolean, namespaceGuessed = false) => ({
  helm: 'helm uninstall ikhnos-agent -n ikhnos-system',
  secret: 'kubectl -n ikhnos-system delete secret ikhnos-agent-identity',
  namespaceGuessed,
  keptByController,
})

describe('what is left in the cluster after a revoke', () => {
  test('an agent a controller keeps is said to be removed by it, with the commands for when it is still there', () => {
    render(<AgentTeardown status="revoked" teardown={teardown(true)} />)
    expect(screen.getByTestId('agent-teardown-controller')).toHaveTextContent(/controller keeps this agent, and removes it now.*nothing to run.*still in the cluster after a few minutes/s)
    expect(screen.getByText(/helm uninstall ikhnos-agent/)).toBeInTheDocument()
    expect(screen.getByText(/delete secret ikhnos-agent-identity/)).toBeInTheDocument()
  })

  test('an agent nothing keeps is told what it always was: nothing was removed', () => {
    render(<AgentTeardown status="revoked" teardown={teardown(false)} />)
    expect(screen.getByTestId('agent-teardown')).toHaveTextContent(/did not remove anything from the cluster/)
    expect(screen.queryByTestId('agent-teardown-controller')).toBeNull()
  })

  test('a rejected enrolment is never said to be removed by a controller, and a guessed namespace is still flagged', () => {
    render(<AgentTeardown status="rejected" teardown={teardown(true, true)} />)
    expect(screen.queryByTestId('agent-teardown-controller')).toBeNull()
    expect(screen.getByTestId('agent-teardown')).toHaveTextContent(/Rejecting stopped this enrollment.*assume namespace ikhnos-system/s)
  })
})
