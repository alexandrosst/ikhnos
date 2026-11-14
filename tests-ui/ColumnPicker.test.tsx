import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { describe, expect, test, vi } from 'vitest'
import ColumnPicker from '@/features/topology/components/ColumnPicker'
import type { ColumnDef } from '@/features/topology/lib/columns'

const COLUMNS: ColumnDef[] = [
  { key: 'cluster', label: 'Cluster' },
  { key: 'ip', label: 'IP' },
]

describe('ColumnPicker', () => {
  test('the button never carries a count, however many columns are hidden', () => {
    render(<ColumnPicker columns={COLUMNS} state={{ isVisible: (k) => k !== 'ip', toggle: () => {} }} />)
    expect(screen.getByTestId('columns-button')).toHaveTextContent(/^Columns$/)
  })

  test('Reset appears only once the choice differs from the default, and calls reset', async () => {
    const user = userEvent.setup()
    const reset = vi.fn()
    const { rerender } = render(<ColumnPicker columns={COLUMNS} state={{ isVisible: () => true, toggle: () => {}, reset, changed: false }} />)
    await user.click(screen.getByTestId('columns-button'))
    expect(screen.queryByTestId('columns-reset')).not.toBeInTheDocument()
    rerender(<ColumnPicker columns={COLUMNS} state={{ isVisible: () => true, toggle: () => {}, reset, changed: true }} />)
    await user.click(screen.getByTestId('columns-reset'))
    expect(reset).toHaveBeenCalledTimes(1)
  })

  test('opening the picker lists one checkbox per column, checked to match isVisible', async () => {
    const user = userEvent.setup()
    render(<ColumnPicker columns={COLUMNS} state={{ isVisible: (k) => k !== 'ip', toggle: () => {} }} />)
    await user.click(screen.getByTestId('columns-button'))
    expect(screen.getByTestId('column-cluster')).toBeChecked()
    expect(screen.getByTestId('column-ip')).not.toBeChecked()
  })

  test('clicking a column\'s checkbox reports that column\'s key, not its checked state', async () => {
    const user = userEvent.setup()
    const onToggle = vi.fn()
    render(<ColumnPicker columns={COLUMNS} state={{ isVisible: () => true, toggle: onToggle }} />)
    await user.click(screen.getByTestId('columns-button'))
    await user.click(screen.getByTestId('column-ip'))
    expect(onToggle).toHaveBeenCalledWith('ip')
  })
})
