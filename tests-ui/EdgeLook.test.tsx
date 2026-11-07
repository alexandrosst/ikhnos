import { ReactFlowProvider } from '@xyflow/react'
import { render, screen } from '@testing-library/react'
import { describe, expect, test } from 'vitest'
import { OffsetEdge, particlesFor, SquareRoutesContext, type RoutedPath } from '@/features/topology/components/OffsetEdge'
import { GroupBox } from '@/features/topology/components/nodes'

const draw = (data: Record<string, unknown>, routed: Partial<RoutedPath> = {}, stroke = '#98a4ae') => {
  const path: RoutedPath = { path: 'M10,20 L10,100 Q10,112 22,112 L300,112', labelX: 0, labelY: 0, length: 400, ...routed }
  const props = { id: 'e1', source: 'a', target: 'b', sourceX: 0, sourceY: 0, targetX: 100, targetY: 0, data, style: { stroke, strokeWidth: 1.8, opacity: 0.4 } } as never
  return render(
    <ReactFlowProvider>
      <SquareRoutesContext.Provider value={new Map([['e1', path]])}>
        <svg><OffsetEdge {...props} /></svg>
      </SquareRoutesContext.Provider>
    </ReactFlowProvider>,
  )
}

describe('a squared line', () => {
  test('has a casing under it and a ring where it leaves its box, both fading with the line', () => {
    const { container } = draw({ observed: false })
    const casing = container.querySelector('.topo-casing')!
    expect(casing.getAttribute('d')).toBe('M10,20 L10,100 Q10,112 22,112 L300,112')
    expect(Number(casing.getAttribute('stroke-width'))).toBeCloseTo(4.8)
    expect((casing as HTMLElement).style.opacity).toBe('0.4')
    const ring = container.querySelector('.topo-port')!
    expect(ring.getAttribute('cx')).toBe('10')
    expect(ring.getAttribute('cy')).toBe('20')
    expect(ring.getAttribute('stroke')).toBe('#98a4ae')
    // The casing sits under the line, and the interaction area is still the line's own.
    const paths = [...container.querySelectorAll('path')]
    expect(paths.indexOf(casing as SVGPathElement)).toBeLessThan(paths.findIndex((p) => p.classList.contains('react-flow__edge-path')))
  })

  test('has no lights when nothing was seen on it, or what was seen has gone quiet', () => {
    expect(draw({ observed: false }).container.querySelector('.topo-flow')).toBeNull()
    expect(draw({ observed: true, stale: true }).container.querySelector('.topo-flow')).toBeNull()
  })

  test('has lights running along its own path when traffic was seen on it, a head and two followers each', () => {
    const { container } = draw({ observed: true, weight: 0.8 }, { length: 400 })
    const flow = container.querySelector('.topo-flow')!
    const motions = flow.querySelectorAll('animateMotion')
    expect(motions.length).toBe(particlesFor(400, 0.8).count * 3)
    expect(motions[0].getAttribute('path')).toBe('M10,20 L10,100 Q10,112 22,112 L300,112')
    expect(flow.querySelector('circle')?.getAttribute('fill')).toBe('var(--color-nb-300)')
  })

  test('a coloured line (amber, red, orange) carries lights of its own colour', () => {
    const { container } = draw({ observed: true }, {}, '#fbbf24')
    expect(container.querySelector('.topo-flow circle')?.getAttribute('fill')).toBe('#fbbf24')
  })

  test('has no lights while its length is not known (before the first measure)', () => {
    expect(draw({ observed: true }, { length: undefined }).container.querySelector('.topo-flow')).toBeNull()
  })
})

describe('particlesFor', () => {
  test('a longer line has more lights, up to five, and a busier one runs them faster', () => {
    expect(particlesFor(60, 0).count).toBe(1)
    expect(particlesFor(450, 0).count).toBe(3)
    expect(particlesFor(5000, 0).count).toBe(5)
    expect(particlesFor(300, 1).speed).toBeGreaterThan(particlesFor(300, 0).speed)
    expect(particlesFor(300, 7).speed).toBe(particlesFor(300, 1).speed)
  })
})

describe('a cluster header', () => {
  const data = { kind: 'group', entityId: 'c1', groupBy: 'cluster', title: 'edge-patras', subtitle: 'k3s · Patras', tier: 'far-edge', status: 'healthy', stats: '2 services', empty: '' }
  test('writes the tier in words, in the tier colour, instead of drawing a badge', () => {
    render(
      <ReactFlowProvider>
        <GroupBox {...({ data, selected: false } as never)} />
      </ReactFlowProvider>,
    )
    const tier = screen.getByTestId('tier-label')
    expect(tier.textContent).toBe('Far edge')
    expect(tier.className).not.toMatch(/rounded|border|bg-/)
    expect((tier as HTMLElement).style.color).toContain('--color-tier-far')
  })
})
