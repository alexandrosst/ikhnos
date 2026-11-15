import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { Callout, Disclosure, DoneDisclosure, LinkButton } from '@/components/ui/primitives'

describe('LinkButton', () => {
  it('is a plain button that never submits a form, in the accent or the quiet tone', () => {
    const onClick = vi.fn()
    render(<><LinkButton onClick={onClick}>Change</LinkButton><LinkButton tone="quiet">Close</LinkButton></>)
    const change = screen.getByRole('button', { name: 'Change' })
    expect(change).toHaveAttribute('type', 'button')
    expect(change.className).toContain('text-accent')
    expect(screen.getByRole('button', { name: 'Close' }).className).toContain('text-nb-500')
    fireEvent.click(change)
    expect(onClick).toHaveBeenCalledOnce()
  })
})

describe('Disclosure', () => {
  it('is closed unless asked, and keeps a person\'s own choice', () => {
    const { rerender } = render(<Disclosure title="Advanced" testId="d">inside</Disclosure>)
    const d = screen.getByTestId('d') as HTMLDetailsElement
    expect(d.open).toBe(false)
    d.open = true
    fireEvent(d, new Event('toggle'))
    rerender(<Disclosure title="Advanced" testId="d">inside</Disclosure>)
    expect((screen.getByTestId('d') as HTMLDetailsElement).open).toBe(true)
  })
  it('follows `open` when the caller owns the state', () => {
    const onOpenChange = vi.fn()
    render(<Disclosure title="Other" open onOpenChange={onOpenChange} testId="o">inside</Disclosure>)
    expect((screen.getByTestId('o') as HTMLDetailsElement).open).toBe(true)
  })
  it('shows a hint beside the title', () => {
    render(<Disclosure title="More" hint="optional">inside</Disclosure>)
    expect(screen.getByText('(optional)')).toBeInTheDocument()
  })
})

describe('DoneDisclosure', () => {
  it('says what was done and folds the content away', () => {
    render(<DoneDisclosure label="Command run." testId="x">the command</DoneDisclosure>)
    expect(screen.getByText('Command run.')).toBeInTheDocument()
    expect((screen.getByTestId('x') as HTMLDetailsElement).open).toBe(false)
  })
})

describe('Callout', () => {
  it('says its tone with an icon, keeps the text readable, and passes role and test id through', () => {
    render(<Callout tone="bad" title="Could not save" role="alert" data-testid="c">Try again.</Callout>)
    const c = screen.getByTestId('c')
    expect(c).toHaveAttribute('role', 'alert')
    expect(c.className).toContain('border-bad/30')
    expect(c.className).toContain('text-nb-300')
    expect(c.querySelector('svg')).not.toBeNull()
    expect(screen.getByText('Could not save')).toBeInTheDocument()
    expect(screen.getByText('Try again.')).toBeInTheDocument()
  })
})
