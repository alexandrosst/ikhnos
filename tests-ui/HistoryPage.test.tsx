import { act, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, test, vi } from 'vitest'
import HistoryPage from '@/features/history/HistoryPage'
import RecordingPanel from '@/features/history/components/RecordingPanel'
import { useRawTopology } from '@/store/topology'
import { DEFAULT_SETTINGS, type AppSettings, type ChangeEvent, type HistoryIndex } from '@/lib/history'

// RecordingPanel is the side panel behind the History page's Recording button. The first block covers the "Link is quiet after" field's unit
// picker (flowStaleSeconds): the backend always stores/validates a plain number of seconds (see backend/internal/server/settings.go), while
// the field lets someone type that number in whichever unit is convenient. The second covers the choices and what a save leaves alone.

let settings: AppSettings
let save: ReturnType<typeof vi.fn>
let role = 'admin'

vi.mock('@/store/settings', () => ({
  useSettings: Object.assign(
    (selector?: (s: unknown) => unknown) => {
      const s = { settings, loaded: true, error: undefined, save }
      return selector ? selector(s) : s
    },
    { getState: () => ({ clear: () => {} }) },
  ),
}))

// The page-level loader below talks to the server through these.
const history = vi.fn()
const events = vi.fn()
const recordNow = vi.fn(async () => undefined)
const audit = vi.fn(async () => ({ source: 'local', rows: [] }))
// One stable object: a fresh one per render would change `load` on every render and refetch forever.
const CONN = { url: 'https://example.test', org: 'o' }
vi.mock('@/store/server', () => ({
  useServer: (selector: (s: unknown) => unknown) => selector({ status: 'connected', role, state: undefined }),
  useConn: () => CONN,
}))
vi.mock('@/lib/api', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/lib/api')>()
  return {
    ...actual,
    api: {
      ...actual.api,
      history: (...a: unknown[]) => history(...a),
      events: (...a: unknown[]) => events(...a),
      recordNow: (...a: unknown[]) => recordNow(...(a as [])),
      audit: (...a: unknown[]) => audit(...a),
      workspaceRevisions: async () => [],
      traffic: async () => ({ hours: 24, snapshots: 0, rates: [] }),
      storage: async () => {
        throw new Error('not under test')
      },
    },
  }
})

function renderField(over: Partial<AppSettings> = {}) {
  settings = { ...DEFAULT_SETTINGS, ...over }
  save = vi.fn().mockResolvedValue(true)
  render(
    <MemoryRouter>
      <RecordingPanel admin conn={{ url: 'https://example.test' }} onClose={() => {}} />
    </MemoryRouter>,
  )
}

describe('RecordingPanel · Link is quiet after (flowStaleSeconds)', () => {
  test('300s shows as "5 minutes", not "300 seconds" or a fraction of an hour', () => {
    renderField({ flowStaleSeconds: 300 })
    expect(screen.getByTestId('setting-flowStaleSeconds')).toHaveValue(5)
    expect(screen.getByTestId('setting-flowStaleUnit')).toHaveValue('60')
  })

  test('a value that does not divide evenly into a larger unit falls back to seconds', () => {
    renderField({ flowStaleSeconds: 97 })
    expect(screen.getByTestId('setting-flowStaleSeconds')).toHaveValue(97)
    expect(screen.getByTestId('setting-flowStaleUnit')).toHaveValue('1')
  })

  test('switching the unit converts the displayed number, keeping the underlying seconds the same', () => {
    renderField({ flowStaleSeconds: 300 })
    fireEvent.change(screen.getByTestId('setting-flowStaleUnit'), { target: { value: '1' } })
    expect(screen.getByTestId('setting-flowStaleSeconds')).toHaveValue(300)
    fireEvent.change(screen.getByTestId('setting-flowStaleUnit'), { target: { value: '3600' } })
    // 300s doesn't divide evenly into hours; the nearest whole hour is what a real person would expect
    // from a unit switch, not a blocked/invalid field.
    expect(screen.getByTestId('setting-flowStaleUnit')).toHaveValue('3600')
  })

  test('typing 0 (or leaving it non-positive) is rejected and blocks Save', () => {
    renderField({ flowStaleSeconds: 300 })
    fireEvent.change(screen.getByTestId('setting-flowStaleSeconds'), { target: { value: '0' } })
    const field = screen.getByTestId('setting-flowStaleSeconds')
    expect(field).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByTestId('save-settings')).toBeDisabled()
    // A screen reader needs the error text itself, not just the invalid flag: aria-describedby must
    // resolve to the hint span that now reads the error message, not the normal explanatory text.
    const describedBy = field.getAttribute('aria-describedby')
    expect(describedBy).toBeTruthy()
    expect(document.getElementById(describedBy!)).toHaveTextContent(/between .* and 30 days/i)
  })

  test('Save sends the product of the typed number and the selected unit, in seconds', async () => {
    renderField({ flowStaleSeconds: 86400 }) // starts at 1 day
    fireEvent.change(screen.getByTestId('setting-flowStaleUnit'), { target: { value: '1' } }) // switch to seconds
    fireEvent.change(screen.getByTestId('setting-flowStaleSeconds'), { target: { value: '90' } })
    fireEvent.click(screen.getByTestId('save-settings'))
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        { url: 'https://example.test' },
        expect.objectContaining({ flowStaleSeconds: 90 }),
      ),
    )
  })
})

describe('RecordingPanel · the choices', () => {
  const pickOf = (name: string, option: string) => {
    fireEvent.click(screen.getByRole('combobox', { name }))
    fireEvent.click(screen.getByRole('option', { name: option }))
  }

  test('offers how often, how long, how big and how long the change log is kept, as choices', () => {
    renderField({ snapshotMinutes: 5, retentionDays: 30, maxHistoryMb: 1024, eventRetentionDays: 0 })
    expect(screen.getByRole('combobox', { name: 'Record every' })).toHaveTextContent('5 minutes')
    expect(screen.getByRole('button', { name: '30 d' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('combobox', { name: 'Size limit' })).toHaveTextContent('1 GB')
    expect(screen.getByRole('combobox', { name: 'Keep the change log' })).toHaveTextContent('Forever')
  })

  test('a value saved earlier that no choice matches is kept as a choice of its own, and opening the panel changes nothing', () => {
    renderField({ snapshotMinutes: 7, retentionDays: 14, maxHistoryMb: 100 })
    expect(screen.getByRole('combobox', { name: 'Record every' })).toHaveTextContent('7 minutes')
    expect(screen.getByRole('button', { name: '14 d' })).toHaveAttribute('aria-pressed', 'true')
    expect(screen.getByRole('combobox', { name: 'Size limit' })).toHaveTextContent('100 MB')
    expect(screen.getByTestId('save-settings')).toBeDisabled()
  })

  test('Save sends the choices and leaves the settings that are not on the screen as they were', async () => {
    renderField({ consistencyMinutes: 17, tombstoneRetentionDays: 11 })
    pickOf('Record every', '15 minutes')
    fireEvent.click(screen.getByRole('button', { name: '90 d' }))
    pickOf('Keep the change log', '90 days')
    fireEvent.click(screen.getByTestId('save-settings'))
    await waitFor(() =>
      expect(save).toHaveBeenCalledWith(
        { url: 'https://example.test' },
        expect.objectContaining({ snapshotMinutes: 15, retentionDays: 90, eventRetentionDays: 90, consistencyMinutes: 17, tombstoneRetentionDays: 11 }),
      ),
    )
  })
})

describe('HistoryPage loading', () => {
  const index: HistoryIndex = { points: [], snapshotMinutes: 5, retentionDays: 30 }
  const ev = (name: string): ChangeEvent => ({ id: name, at: '2026-01-02T03:04:05Z', kind: 'cluster-added', targetKind: 'service', targetId: name, name, detail: '', severity: 'info' })
  // The Select is a custom listbox, not a native <select>: open it and pick the option.
  const pickCluster = (label: string) => {
    fireEvent.click(screen.getByRole('combobox', { name: 'Cluster' }))
    fireEvent.click(screen.getByRole('option', { name: label }))
  }
  useRawTopology.setState({ clusters: [{ id: 'c1', name: 'alpha' }] } as never)
  const renderPage = () => render(<MemoryRouter><HistoryPage /></MemoryRouter>)

  test('a filter change after a refresh sends one request per source, not two', async () => {
    settings = { ...DEFAULT_SETTINGS }
    history.mockReset().mockResolvedValue(index)
    events.mockReset().mockResolvedValue([])
    recordNow.mockClear()
    renderPage()
    await waitFor(() => expect(events).toHaveBeenCalledTimes(1))

    // "Record now" bumps the tick the cards below depend on; before the fix that also armed a second loader effect.
    fireEvent.click(await screen.findByRole('button', { name: /record now/i }))
    await waitFor(() => expect(events).toHaveBeenCalledTimes(2))
    expect(history).toHaveBeenCalledTimes(2)

    history.mockClear()
    events.mockClear()
    pickCluster('alpha')
    await waitFor(() => expect(events).toHaveBeenCalled())
    await act(async () => {
      await new Promise((r) => setTimeout(r, 50))
    })
    expect(events).toHaveBeenCalledTimes(1)
    expect(history).toHaveBeenCalledTimes(1)
  })

  test('a slow answer for an earlier filter does not replace the changes for the current one', async () => {
    settings = { ...DEFAULT_SETTINGS }
    history.mockReset().mockResolvedValue(index)
    let finishOld!: (v: ChangeEvent[]) => void
    events.mockReset().mockImplementationOnce(() => new Promise<ChangeEvent[]>((res) => { finishOld = res }))
    renderPage()
    await waitFor(() => expect(events).toHaveBeenCalledTimes(1)) // the unfiltered request, still out

    events.mockResolvedValueOnce([ev('newer-change')])
    pickCluster('alpha')
    await screen.findByText('newer-change')

    await act(async () => {
      finishOld([ev('older-change')])
    })
    expect(screen.queryByText('older-change')).toBeNull()
    expect(screen.getByText('newer-change')).toBeInTheDocument()
  })
})

describe('HistoryPage views', () => {
  const index: HistoryIndex = { points: [], snapshotMinutes: 5, retentionDays: 30 }
  const renderAt = (url = '/history') => render(<MemoryRouter initialEntries={[url]}><HistoryPage /></MemoryRouter>)
  const setup = (r = 'admin') => {
    role = r
    settings = { ...DEFAULT_SETTINGS }
    history.mockReset().mockResolvedValue(index)
    events.mockReset().mockResolvedValue([])
    audit.mockClear()
  }

  test('an administrator gets Changes and People, and opens on Changes without asking for the audit trail', async () => {
    setup()
    renderAt()
    const group = await screen.findByRole('group', { name: 'View' })
    expect(Array.from(group.querySelectorAll('button')).map((b) => b.textContent)).toEqual(['Changes', 'People'])
    await waitFor(() => expect(events).toHaveBeenCalled())
    expect(audit).not.toHaveBeenCalled()
  })

  test('People shows who did what, with its filters and the CSV export, and Changes brings the feed back', async () => {
    setup()
    renderAt()
    fireEvent.click(await screen.findByRole('button', { name: 'People' }))
    await waitFor(() => expect(audit).toHaveBeenCalled())
    expect(screen.getByTestId('audit-actor')).toBeInTheDocument()
    expect(screen.getByTestId('audit-download-csv')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: /record now/i })).toBeNull()
    fireEvent.click(screen.getByRole('button', { name: 'Changes' }))
    expect(await screen.findByRole('button', { name: /record now/i })).toBeInTheDocument()
    expect(screen.queryByTestId('audit-actor')).toBeNull()
  })

  test('the address says which view, so /history?view=people opens People', async () => {
    setup()
    renderAt('/history?view=people')
    await waitFor(() => expect(audit).toHaveBeenCalled())
    expect(screen.getByTestId('audit-actor')).toBeInTheDocument()
  })

  test('someone who is not an administrator has only Changes, even at the People address', async () => {
    setup('viewer')
    renderAt('/history?view=people')
    await waitFor(() => expect(events).toHaveBeenCalled())
    expect(screen.queryByRole('group', { name: 'View' })).toBeNull()
    expect(screen.queryByTestId('audit-actor')).toBeNull()
    expect(audit).not.toHaveBeenCalled()
  })
})
