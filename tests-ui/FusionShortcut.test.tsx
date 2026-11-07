import { render, screen } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { MemoryRouter } from 'react-router-dom'
import { beforeEach, describe, expect, test, vi } from 'vitest'
import FusionShortcut from '@/features/fusion/components/FusionShortcut'

// The sidebar's chart button: Grafana in a new tab, only for someone who may use FUSION and only while Grafana is up.

let admin = true
let links: { grafana?: string; prometheus?: string } | undefined
const open = vi.fn()
const refresh = vi.fn()
let error = ''
vi.mock('@/store/server', () => ({ useServer: (sel: (s: { isAdmin: () => boolean }) => unknown) => sel({ isAdmin: () => admin }) }))
vi.mock('@/features/fusion/components/useFusion', () => ({ useFusion: (enabled: boolean) => ({ status: enabled ? { links } : null, refresh }) }))
vi.mock('@/features/fusion/components/OpenPages', () => ({ useOpenPage: () => ({ open, opening: false, error }) }))

const renderIt = () => render(<MemoryRouter><FusionShortcut /></MemoryRouter>)

beforeEach(() => {
  admin = true
  links = { grafana: '/g', prometheus: '/p' }
  error = ''
  open.mockClear()
  refresh.mockClear()
})

describe('FusionShortcut', () => {
  test('opens Grafana from the sidebar', async () => {
    renderIt()
    await userEvent.click(screen.getByRole('button', { name: 'Open Grafana in a new tab' }))
    expect(open).toHaveBeenCalledWith('grafana')
  })

  test('is not there while Grafana is not up', () => {
    links = { prometheus: '/p' }
    renderIt()
    expect(screen.queryByTestId('fusion-shortcut')).toBeNull()
    links = undefined
    renderIt()
    expect(screen.queryByTestId('fusion-shortcut')).toBeNull()
  })

  test('is not there for someone who may not use FUSION', () => {
    admin = false
    renderIt()
    expect(screen.queryByTestId('fusion-shortcut')).toBeNull()
  })

  test('reads FUSION\'s state again when the page changes, and says why a tab did not open', () => {
    error = 'Your browser blocked the new tab.'
    renderIt()
    expect(refresh).toHaveBeenCalled()
    expect(screen.getByRole('alert')).toHaveTextContent('blocked the new tab')
    expect(screen.getByTestId('fusion-shortcut')).toHaveAttribute('title', 'Your browser blocked the new tab.')
  })
})
