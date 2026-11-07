import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { WizardSteps } from '@/components/ui/primitives'

const STEPS = ['Connect', 'Approve', 'Discover', 'Done']

/** The colored circle for a step: whichever element carries both the rounded-full badge classes. */
const circleFor = (label: string) =>
  Array.from(screen.getByText(label).closest('div')!.querySelectorAll('span')).find(
    (el) => el.className.includes('rounded-full') && !el.className.includes('animate-ping'),
  )!

describe('WizardSteps', () => {
  test('marks steps before currentIndex done, the one at it current, and the rest upcoming', () => {
    render(<WizardSteps steps={STEPS} currentIndex={1} />)
    expect(circleFor('Connect').className).toContain('border-ok/50')
    expect(circleFor('Connect').textContent).toBe('') // a checkmark icon, not the step number
    expect(screen.getByText('Connect').className).toContain('text-nb-400')

    expect(circleFor('Approve').className).toContain('border-accent')
    expect(screen.getByText('Approve').className).toContain('text-nb-200')

    expect(circleFor('Discover').className).toContain('border-nb-800')
    expect(circleFor('Discover').textContent).toBe('3')
    expect(screen.getByText('Discover').className).toContain('text-nb-600')

    expect(circleFor('Done').className).toContain('border-nb-800')
    expect(circleFor('Done').textContent).toBe('4')
  })

  test('a failed step overrides done/current styling, and nothing after it is implied to have happened', () => {
    // Mirrors how ConnectClusterWizard actually calls this when phase is 'stopped': the step that failed
    // is passed as both currentIndex and failedIndex, so nothing past it reads as current or done either.
    render(<WizardSteps steps={STEPS} currentIndex={2} failedIndex={2} />)
    expect(circleFor('Connect').className).toContain('border-ok/50')
    expect(circleFor('Approve').className).toContain('border-ok/50')

    expect(circleFor('Discover').className).toContain('border-bad/50')
    expect(screen.getByText('Discover').className).toContain('text-bad')

    expect(circleFor('Done').className).toContain('border-nb-800')
    expect(circleFor('Done').textContent).toBe('4')
  })

  test('defaults to the wizard-steps testid, overridable per caller', () => {
    const { rerender } = render(<WizardSteps steps={STEPS} currentIndex={0} />)
    expect(screen.getByTestId('wizard-steps')).toBeInTheDocument()
    rerender(<WizardSteps steps={STEPS} currentIndex={0} testId="guided-steps" />)
    expect(screen.getByTestId('guided-steps')).toBeInTheDocument()
  })

  test('the lines between the steps all stretch by the same share, so the gaps are equal whatever the labels are', () => {
    render(<WizardSteps steps={['Set up', 'Agent connects', 'You approve it', 'Discovering', 'Connected']} currentIndex={0} />)
    const row = screen.getByTestId('wizard-steps')
    const lines = [...row.children].filter((el) => el.className.includes('h-px'))
    expect(lines).toHaveLength(4)
    for (const l of lines) expect(l.className).toContain('flex-1')
    // The steps themselves do not stretch with their own line.
    for (const step of [...row.children].filter((el) => !el.className.includes('h-px'))) expect(step.className).not.toContain('flex-1')
  })
})
