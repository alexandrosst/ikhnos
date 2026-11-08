import { fireEvent, render, screen, within } from '@testing-library/react'
import { MemoryRouter } from 'react-router-dom'
import { describe, expect, test } from 'vitest'
import Feed from '@/features/history/components/Feed'
import { actionWhat, buildFeed, groupByDay } from '@/features/history/lib/feed'
import type { ChangeEvent } from '@/lib/history'
import type { AuditEvent } from '@/lib/types'

const ev = (id: string, at: string, over: Partial<ChangeEvent> = {}): ChangeEvent => ({ id, at, kind: 'service-scaled', targetKind: 'service', targetId: id, name: id, detail: '', severity: 'info', ...over })
const act = (id: string, at: string, over: Partial<AuditEvent> = {}): AuditEvent => ({ id, orgId: 'o', at, actor: 'alexandros', action: 'edit', targetKind: 'cluster', targetId: 'c1', detail: 'Provider: AWS → Amazon Web Services', ...over })
const NOW = new Date(2026, 9, 5, 15, 0).getTime()
const at = (daysAgo: number, h = 12) => new Date(2026, 9, 5 - daysAgo, h, 0).toISOString()

describe('feed', () => {
  test('changes and actions share one list, newest first', () => {
    const items = buildFeed([ev('a', at(0, 9)), ev('c', at(1, 9))], [act('b', at(0, 11))], { filter: 'all' })
    expect(items.map((i) => i.id)).toEqual(['a:b', 'e:a', 'e:c'])
  })

  test('the filter picks what Ikhnos noticed or what people did', () => {
    const e = [ev('a', at(0))]
    const a = [act('b', at(0))]
    expect(buildFeed(e, a, { filter: 'changes' }).map((i) => i.type)).toEqual(['change'])
    expect(buildFeed(e, a, { filter: 'people' }).map((i) => i.type)).toEqual(['action'])
  })

  test('actions are narrowed by the window and by the cluster they were done to; changes arrive already narrowed', () => {
    const a = [act('old', at(10)), act('here', at(0)), act('other', at(0), { targetId: 'c2' })]
    const items = buildFeed([], a, { filter: 'all', since: NOW - 24 * 3600_000, clusterId: 'c1' })
    expect(items.map((i) => i.id)).toEqual(['a:here'])
  })

  test('an action reads as a sentence: who, what, and what it touched when the verb alone would not say', () => {
    expect(actionWhat({ action: 'edit', targetKind: 'cluster' })).toBe('edited cluster')
    expect(actionWhat({ action: 'accept-suggestion', targetKind: 'suggestion' })).toBe('accepted a suggestion')
    expect(actionWhat({ action: 'future-thing', targetKind: 'node' })).toBe('future thing')
  })

  test('days are Today, Yesterday, then by date', () => {
    const groups = groupByDay([{ at: at(0, 14) }, { at: at(0, 9) }, { at: at(1) }, { at: at(5) }], NOW)
    expect(groups.map((g) => g.label).slice(0, 2)).toEqual(['Today', 'Yesterday'])
    expect(groups[0].items).toHaveLength(2)
    expect(groups).toHaveLength(3)
  })
})

describe('Feed', () => {
  const draw = (events: ChangeEvent[], audit: AuditEvent[] = [], filter: 'all' | 'changes' | 'people' = 'all') =>
    render(<MemoryRouter><Feed events={events} audit={audit} filter={filter} clusterId="" since={0} hours={24} /></MemoryRouter>)

  test('a change names what changed, how, where, and the likely cause; a person is named with what they did', () => {
    draw([ev('kafka', at(0, 14), { kind: 'service-scaled', detail: '2 → 3 replicas', clusterName: 'athens', cause: 'a node joined', severity: 'warning' })], [act('x', at(0, 13))])
    const rows = screen.getAllByTestId(/event-row|audit-row/)
    expect(rows).toHaveLength(2)
    expect(within(rows[0]).getByText('kafka')).toBeInTheDocument()
    expect(rows[0]).toHaveTextContent('Service scaled · athens')
    expect(rows[0]).toHaveTextContent('2 → 3 replicas')
    expect(rows[0]).toHaveTextContent('Likely cause: a node joined')
    expect(rows[1]).toHaveTextContent('alexandros edited cluster')
  })

  test('shows twenty at a time and adds twenty more on request', () => {
    const many = Array.from({ length: 45 }, (_, i) => ev(`e${i}`, new Date(NOW - i * 60_000).toISOString()))
    draw(many)
    expect(screen.getAllByTestId('event-row')).toHaveLength(20)
    fireEvent.click(screen.getByTestId('feed-more'))
    expect(screen.getAllByTestId('event-row')).toHaveLength(40)
    fireEvent.click(screen.getByTestId('feed-more'))
    expect(screen.getAllByTestId('event-row')).toHaveLength(45)
    expect(screen.queryByTestId('feed-more')).toBeNull()
  })

  test('an empty window says so, for the filter chosen', () => {
    draw([], [], 'people')
    expect(screen.getByText(/No actions by people were recorded/)).toBeInTheDocument()
  })
})
