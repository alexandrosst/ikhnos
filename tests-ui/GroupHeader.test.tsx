import { ReactFlowProvider } from '@xyflow/react'
import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { GroupBox } from '@/features/topology/components/nodes'
import type { GroupData } from '@/features/topology/lib/graph'

const group = (over: Partial<GroupData> = {}): GroupData => ({
  kind: 'group', entityId: 'a', groupBy: 'cluster', title: 'edge-a', subtitle: 'k3s \u00b7 Athens', tier: 'edge', status: 'ready', stats: '2 nodes', empty: 'No nodes', ...over,
})
const load = (over: Record<string, number>) => ({ nodes: 2, ready: 2, unready: 0, ...over }) as NonNullable<GroupData['load']>
const draw = (data: GroupData) => render(<ReactFlowProvider><GroupBox {...({ data, selected: false } as never)} /></ReactFlowProvider>)

describe('a cluster box header', () => {
  test('says the name, where it is, and how many nodes - and nothing about the network or the operator', () => {
    draw(group())
    expect(screen.getByText('edge-a')).toBeInTheDocument()
    expect(screen.getByText('k3s \u00b7 Athens')).toBeInTheDocument()
    expect(screen.getByText('2 nodes')).toBeInTheDocument()
    expect(screen.queryByTestId('net-chip')).not.toBeInTheDocument()
    expect(screen.queryByTestId('networking-badge')).not.toBeInTheDocument()
    expect(screen.queryByTestId('local-telemetry-badge')).not.toBeInTheDocument()
    expect(screen.queryByTestId('cluster-load')).not.toBeInTheDocument()
  })

  test('shows the busiest resource only once it is worth a look (70% and over)', () => {
    const { unmount } = draw(group({ load: load({ cpuPct: 40, memPct: 69 }) }))
    expect(screen.queryByTitle(/Busiest resource/)).not.toBeInTheDocument()
    unmount()
    draw(group({ load: load({ cpuPct: 40, memPct: 82 }) }))
    expect(screen.getByTitle(/Busiest resource/)).toHaveTextContent('82%')
  })
})
