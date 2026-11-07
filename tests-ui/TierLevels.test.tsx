import { render, screen, within } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import TierLevels, { PLAIN_READS } from '@/components/TierLevels'
import { ACCESS_TIER_GRANTS } from '@/lib/types'

// The wizard's cards say what each level can read once, in plain words, instead of a disclosure under each card. Those words must not say
// more than the permissions the chart really grants (ACCESS_TIER_GRANTS), and the screens that stack the levels keep the exact lines.

describe('TierLevels as cards', () => {
  test('one disclosure for both levels, not one under each card', () => {
    render(<TierLevels tiers={[1, 2]} value={2} onSelect={() => undefined} layout="cards" data-testid="t" />)
    expect(screen.getAllByText('Exactly what each level can read')).toHaveLength(1)
    expect(screen.queryByText('What this grants')).toBeNull()
    const reads = screen.getByTestId('t-reads')
    expect(reads).not.toHaveAttribute('open')
    expect(reads).toHaveTextContent('Infrastructure: nodes, storage classes, ingress classes.')
    expect(reads).toHaveTextContent('Services adds: namespaces, pods, services')
    expect(reads).toHaveTextContent('Both read only (get, list, watch)')
  })

  test('with only the lower level there is nothing it "adds"', () => {
    render(<TierLevels tiers={[1]} value={1} layout="cards" data-testid="t" />)
    const reads = screen.getByTestId('t-reads')
    expect(reads).toHaveTextContent('Infrastructure: nodes')
    expect(reads).not.toHaveTextContent('Services')
  })

  test('what the wizard says goes between the cards and the disclosure', () => {
    render(<TierLevels tiers={[1, 2]} value={2} layout="cards" data-testid="t" after={<p>Always read-only.</p>} />)
    const note = screen.getByText('Always read-only.')
    const reads = screen.getByTestId('t-reads')
    expect(note.compareDocumentPosition(reads) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
  })

  test('the stacked levels keep the exact grants under each one', () => {
    render(<TierLevels tiers={[1, 2]} value={2} data-testid="t" />)
    expect(screen.getAllByText('What this grants')).toHaveLength(2)
    expect(within(screen.getByTestId('t-2-grants')).getByText(/get\/list\/watch deployments/)).toBeInTheDocument()
  })
})

describe('the plain words for what each level reads', () => {
  test.each([1, 2] as const)('every line for level %i names something that level really grants, and its lower levels do not', (tier) => {
    const granted = ACCESS_TIER_GRANTS[tier].grants.join('\n')
    for (const r of PLAIN_READS[tier]) expect(granted, `${r.text} (${r.match})`).toContain(r.match)
  })

  test('what is said to be added by Services is not already granted by Infrastructure', () => {
    const lower = ACCESS_TIER_GRANTS[1].grants.join('\n')
    for (const r of PLAIN_READS[2]) expect(lower, r.text).not.toContain(r.match)
  })
})

describe('TierLevels as a row', () => {
  test('the levels sit side by side with a few words each, and one line for what they read', () => {
    render(<TierLevels tiers={[0, 1, 2]} value={1} max={2} onSelect={() => undefined} layout="row" data-testid="t" />)
    expect(screen.getAllByRole('radio')).toHaveLength(3)
    expect(screen.getByTestId('t-1')).toHaveAttribute('aria-checked', 'true')
    expect(screen.getByTestId('t-2')).toHaveTextContent('Workloads and services')
    expect(screen.getAllByText('Exactly what each level can read')).toHaveLength(1)
    expect(screen.queryByText('What this grants')).toBeNull()
  })

  test('a level above the ceiling is locked and still reports its click', () => {
    const picked: number[] = []
    render(<TierLevels tiers={[0, 1, 2]} value={0} max={0} onSelect={(t) => picked.push(t)} layout="row" data-testid="t" />)
    expect(screen.getByTestId('t-2')).toHaveAttribute('aria-disabled', 'true')
    screen.getByTestId('t-2').click()
    expect(picked).toEqual([2])
  })
})
