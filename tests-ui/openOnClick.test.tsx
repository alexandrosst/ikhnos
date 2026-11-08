import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import { openOnClick } from '@/features/topology/shared'

function Row({ onOpen }: { onOpen: () => void }) {
  return (
    <table>
      <tbody>
        <tr {...openOnClick(onOpen)}>
          <td>edge-1</td>
          <td><a href="#x">link</a></td>
          <td><button>Delete</button></td>
        </tr>
      </tbody>
    </table>
  )
}

describe('openOnClick', () => {
  test('a click on the row opens the panel', () => {
    const onOpen = vi.fn()
    render(<Row onOpen={onOpen} />)
    fireEvent.click(screen.getByText('edge-1'))
    expect(onOpen).toHaveBeenCalledOnce()
  })

  test('a click on a link or button inside the row is left to it', () => {
    const onOpen = vi.fn()
    render(<Row onOpen={onOpen} />)
    fireEvent.click(screen.getByRole('link'))
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onOpen).not.toHaveBeenCalled()
  })
})
