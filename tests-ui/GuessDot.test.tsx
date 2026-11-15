import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { GuessDot } from '@/components/ui/primitives'

describe('GuessDot', () => {
  test('a guess is a dot with the reason in its tooltip, and no words', () => {
    const { container } = render(<GuessDot level="guess" why="from the node count" />)
    expect(screen.getByRole('img', { name: 'Not certain' })).toHaveAttribute('title', 'Probably right, not certain. from the node count')
    expect(container).toHaveTextContent('')
  })

  test('an unknown value gets nothing: the cell is simply empty', () => {
    const { container } = render(<GuessDot level="unknown" why="never reported" />)
    expect(container).toBeEmptyDOMElement()
  })

  test('it keeps its own gap from the value, except where the parent already spaces its children', () => {
    const { rerender } = render(<GuessDot />)
    expect(screen.getByRole('img', { name: 'Not certain' })).toHaveClass('ml-2')
    rerender(<GuessDot flush />)
    expect(screen.getByRole('img', { name: 'Not certain' })).not.toHaveClass('ml-2')
  })
})
