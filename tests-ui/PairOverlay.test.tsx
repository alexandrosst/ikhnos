import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, test, vi } from 'vitest'
import { PairOverlayBox } from '@/features/links/components/PairOverlayBox'
import { OverlaySection } from '@/features/topology/components/OverlaySection'
import { pairOverlay } from '@/lib/overlay'
import type { Agent, Overlay } from '@/lib/types'

// How two clusters are connected, as far as their nodes say. Plain statements: no badge says "likely" or "guess"; when mesh addresses are worth
// measuring through there is a button that picks them, and nothing is chosen for the person.

const ts = (range: string, nodes = 3, ofNodes = 4): Overlay => ({ product: 'tailscale', interface: 'tailscale0', kind: 'tun', range, nodes, ofNodes, why: ['Interface tailscale0 on 3 of 4 nodes.', 'Its addresses are inside ' + range + '.'] })
const bare = (range: string): Overlay => ({ range, nodes: 2, ofNodes: 3, why: ['two of three nodes have an address in 100.64.0.0/10'] })
const ag = (clusterId: string, overlay?: Overlay, peers: Agent['overlayPeers'] = []) => ({ id: 'ag-' + clusterId, clusterId, status: 'approved', overlay, overlayPeers: peers }) as unknown as Agent
const name = (id: string) => ({ 'c-a': 'edge-lab', 'c-b': 'core', 'c-c': 'branch-7' })[id] ?? id

describe('what is said about a pair', () => {
  const same = [ag('c-a', ts('100.64.12.0/24'), [{ clusterId: 'c-b' }]), ag('c-b', ts('100.64.7.0/24', 2, 2), [{ clusterId: 'c-a' }])]

  test('two clusters on the same product: each one, and one sentence that suggests measuring through mesh addresses', () => {
    const p = pairOverlay(same, 'c-a', 'c-b', name)!
    expect(p.rows.map((r) => [r.name, r.what, r.range, r.nodes])).toEqual([
      ['edge-lab', 'Tailscale', '100.64.12.0/24', '3 of 4 nodes'],
      ['core', 'Tailscale', '100.64.7.0/24', 'all 2 nodes'],
    ])
    expect(p.sentence).toBe('Likely the same Tailscale network. Measure through mesh addresses to confirm.')
    expect(p.offerMesh).toBe(true)
    expect(p.meshLabel).toBe('Mesh addresses: Tailscale, both clusters (suggested)')
  })

  test('a connection that opened to a mesh address replaces "likely" with what happened', () => {
    const p = pairOverlay(same, 'c-a', 'c-b', name, ['mesh', 'public'])!
    expect(p.sentence).toBe('Connections between them opened over mesh addresses.')
    expect(p.meshLabel).toBe('Mesh addresses: Tailscale, both clusters')
    expect(pairOverlay(same, 'c-a', 'c-b', name, ['public'])!.sentence).toContain('Likely the same Tailscale network')
  })

  test('the same for a maybe: reaching the range settles it, whatever the range was used for', () => {
    const agents = [ag('c-a', ts('100.64.12.0/24'), [{ clusterId: 'c-c', maybe: true }]), ag('c-c', bare('100.64.0.0/10'))]
    expect(pairOverlay(agents, 'c-a', 'c-c', name, ['mesh'])!.sentence).toBe('Connections between them opened over mesh addresses.')
  })

  test('one side with only addresses in the shared range is a maybe, named, with what to do', () => {
    const agents = [ag('c-a', ts('100.64.12.0/24'), [{ clusterId: 'c-c', maybe: true }]), ag('c-c', bare('100.64.0.0/10'))]
    const p = pairOverlay(agents, 'c-a', 'c-c', name)!
    expect(p.rows[1]).toMatchObject({ name: 'branch-7', what: 'Addresses in', note: 'no overlay interface seen' })
    expect(p.sentence).toContain('branch-7 may be on the same network, or on a carrier’s shared address space')
    expect(p.sentence).toContain('node probe')
    expect(p.meshLabel).toBeUndefined()
  })

  test('two different products are said not to share a network, and nothing is offered', () => {
    const nb: Overlay = { ...ts('100.64.9.0/24'), product: 'netbird', interface: 'wt0' }
    const p = pairOverlay([ag('c-a', ts('100.64.12.0/24')), ag('c-b', nb)], 'c-a', 'c-b', name)!
    expect(p.sentence).toBe('They are not on the same overlay network.')
    expect(p.offerMesh).toBe(false)
  })

  test('nothing is said when either has no overlay, or is not approved', () => {
    expect(pairOverlay([ag('c-a', ts('100.64.12.0/24')), ag('c-b')], 'c-a', 'c-b', name)).toBeNull()
    expect(pairOverlay([ag('c-a'), ag('c-b')], 'c-a', 'c-b', name)).toBeNull()
    expect(pairOverlay([ag('c-a', ts('100.64.12.0/24')), { ...ag('c-b', ts('100.64.1.0/24')), status: 'pending' } as Agent], 'c-a', 'c-b', name)).toBeNull()
  })
})

describe('the box in the dialog', () => {
  const agents = [ag('c-a', ts('100.64.12.0/24'), [{ clusterId: 'c-b' }]), ag('c-b', ts('100.64.7.0/24'), [{ clusterId: 'c-a' }])]

  test('has a button that picks mesh addresses, and it goes away once they are picked', () => {
    const use = vi.fn()
    const { rerender } = render(<PairOverlayBox agents={agents} x="c-a" y="c-b" name={name} via="" onUseMesh={use} />)
    fireEvent.click(screen.getByTestId('pair-overlay-use'))
    expect(use).toHaveBeenCalledTimes(1)
    rerender(<PairOverlayBox agents={agents} x="c-a" y="c-b" name={name} via="mesh" onUseMesh={use} />)
    expect(screen.queryByTestId('pair-overlay-use')).toBeNull()
    expect(screen.getByTestId('pair-overlay')).toHaveTextContent('How these two are connected')
  })

  test('is not there when there is nothing to say', () => {
    render(<PairOverlayBox agents={[ag('c-a'), ag('c-b')]} x="c-a" y="c-b" name={name} via="" onUseMesh={() => {}} />)
    expect(screen.queryByTestId('pair-overlay')).toBeNull()
  })

  test('carries no badge', () => {
    render(<PairOverlayBox agents={agents} x="c-a" y="c-b" name={name} via="" onUseMesh={() => {}} />)
    expect(screen.queryByText(/^(guess|likely|detected|confirmed|estimated)$/i)).toBeNull()
  })
})

describe('the section of a cluster', () => {
  test('says the product, range, nodes and who shares it, and keeps what it rests on behind one click', () => {
    render(<OverlaySection overlay={ts('100.64.12.0/24')} sharedWith={['core']} />)
    expect(screen.getByTestId('overlay-name')).toHaveTextContent('Tailscale')
    expect(screen.getByText('100.64.12.0/24')).toBeVisible()
    expect(screen.getByText('3 of 4 nodes')).toBeVisible()
    expect(screen.getByText('core')).toBeVisible()
    expect(screen.queryByTestId('overlay-evidence')).toBeNull()
    fireEvent.click(screen.getByTestId('overlay-why'))
    expect(screen.getByTestId('overlay-evidence')).toHaveTextContent('Interface tailscale0 on 3 of 4 nodes.')
    expect(screen.getByTestId('overlay-evidence')).toHaveTextContent('the link measurement shows that')
  })

  test('an overlay with no product is named by its interface, and "Also in" is left out when nobody shares it', () => {
    render(<OverlaySection overlay={{ ...ts('10.9.0.0/24'), product: undefined, interface: 'mesh0' }} sharedWith={[]} />)
    expect(screen.getByTestId('overlay-name')).toHaveTextContent('mesh0')
    expect(screen.queryByText('Also in')).toBeNull()
  })
})
