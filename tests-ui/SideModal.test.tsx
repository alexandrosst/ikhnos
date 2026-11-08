import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { Modal } from '@/components/ui/primitives'

const draw = (side: boolean) =>
  render(
    <Modal open side={side} onClose={() => {}} title="Cluster link" footer={<button>Save</button>}>
      <p>The form</p>
    </Modal>,
  )

describe('a side panel', () => {
  test('is exactly the screen tall: the form scrolls between a header and buttons that stay put', () => {
    draw(true)
    const panel = screen.getByRole('dialog')
    expect(panel.className).toContain('h-full')
    expect(panel.className).not.toContain('min-h-full')
    expect(panel.parentElement?.className).toContain('overflow-hidden')
    const body = screen.getByText('The form').parentElement as HTMLElement
    expect(body.className).toContain('overflow-y-auto')
    expect(body.contains(screen.getByText('Save'))).toBe(false)
  })

  test('a centred dialog still scrolls as a whole', () => {
    draw(false)
    expect(screen.getByRole('dialog').parentElement?.className).toContain('overflow-y-auto')
  })
})
