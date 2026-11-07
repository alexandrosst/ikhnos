import { render, screen } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, test } from 'vitest'
import DataPathLine from '@/features/pipeline/components/telemetry/DataPathLine'

// "Unknown" next to a sentence that already says what is known is noise: a state that is not known is not said.

describe('DataPathLine', () => {
  test('says nothing of its health while it is not known, and still leads to the data path', () => {
    render(
      <MemoryRouter>
        <DataPathLine installed={['resourceUsage']} cluster="edge-1" agentId="a1" />
      </MemoryRouter>,
    )
    expect(screen.getByTestId('data-path-line')).toHaveAttribute('data-health', 'unknown')
    expect(screen.queryByText('Unknown')).toBeNull()
    expect(screen.getByTestId('data-path-link')).toBeInTheDocument()
  })
})
