import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import { editOnDoubleClick } from '@/features/topology/shared'

function Row({ onEdit }: { onEdit: () => void }) {
  return (
    <table>
      <tbody>
        <tr {...editOnDoubleClick(onEdit)}>
          <td>edge-1</td>
          <td><a href="#x">link</a></td>
          <td><button>Delete</button></td>
        </tr>
      </tbody>
    </table>
  )
}

describe('editOnDoubleClick', () => {
  test('a double-click on the row opens the editor; a single click does not', () => {
    const onEdit = vi.fn()
    render(<Row onEdit={onEdit} />)
    fireEvent.click(screen.getByText('edge-1'))
    expect(onEdit).not.toHaveBeenCalled()
    fireEvent.doubleClick(screen.getByText('edge-1'))
    expect(onEdit).toHaveBeenCalledOnce()
  })

  test('a double-click on a link or button inside the row is left to it', () => {
    const onEdit = vi.fn()
    render(<Row onEdit={onEdit} />)
    fireEvent.doubleClick(screen.getByRole('link'))
    fireEvent.doubleClick(screen.getByRole('button', { name: 'Delete' }))
    expect(onEdit).not.toHaveBeenCalled()
  })
})
