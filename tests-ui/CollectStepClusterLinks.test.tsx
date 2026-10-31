import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { describe, expect, it } from 'vitest'
import CollectStep from '@/features/pipeline/components/telemetry/CollectStep'
import { LINK_REQUIREMENT } from '@/lib/clusterLinks'
import { emptyTelemetry, isKept, keepAsInstalled, telemetryProblems, type TelemetryInput } from '@/lib/install'

function Harness({ onValue, initial }: { onValue: (v: TelemetryInput) => void; initial?: TelemetryInput }) {
  const [v, setV] = useState<TelemetryInput>(initial ?? { ...emptyTelemetry, exportEndpoint: 'x:4317' })
  onValue(v)
  return <CollectStep value={v} onChange={setV} testIdPrefix="t" />
}

describe('CollectStep: Cluster links', () => {
  it('offers probing and throughput only once the signal is on; the Secret is optional and tucked away', () => {
    let latest = emptyTelemetry
    render(<Harness onValue={(v) => (latest = v)} />)
    expect(screen.queryByTestId('t-links-probe')).toBeNull()
    fireEvent.click(screen.getByTestId('t-clusterLinks'))
    expect(latest.clusterLinks).toBe(true)
    expect(screen.getByTestId('t-links-probe')).toBeChecked()
    // The ports and the no-NAT rule are what cost the most debugging when missed, so they are said where probing is chosen.
    expect(screen.getByTestId('t-links-probe').closest('label')).toHaveTextContent(/TCP 8861 and UDP 18900–18919 with no address translation \(NAT\)/)
    expect(screen.queryByTestId('t-links-own-credentials')).toBeNull()
    fireEvent.click(screen.getByTestId('t-links-throughput'))
    // Nothing is required: Ikhnos issues the credentials, and the field sits closed under an Advanced disclosure.
    expect(telemetryProblems(latest)).toEqual([])
    const details = screen.getByTestId('t-links-own-credentials') as HTMLDetailsElement
    expect(details.open).toBe(false)
    expect(details).toHaveTextContent('Advanced: use my own iperf3 credentials')
    expect(details).toHaveTextContent(/Ikhnos issues the credentials/)
    fireEvent.change(screen.getByTestId('t-links-secret'), { target: { value: 'Bad_Name' } })
    expect(telemetryProblems(latest).join(' ')).toMatch(/credential Secret name/)
    fireEvent.change(screen.getByTestId('t-links-secret'), { target: { value: 'ikhnos-link-iperf' } })
    expect(telemetryProblems(latest)).toEqual([])
    expect(latest.clusterLinksAuthSecret).toBe('ikhnos-link-iperf')
    fireEvent.click(screen.getByTestId('t-links-probe'))
    expect(screen.queryByTestId('t-links-throughput')).toBeNull()
  })

  it('says what a pair needs, in one sentence, as soon as the signal is on', () => {
    render(<Harness onValue={() => {}} />)
    expect(screen.queryByTestId('t-links-requirement')).toBeNull()
    fireEvent.click(screen.getByTestId('t-clusterLinks'))
    expect(screen.getByTestId('t-links-requirement')).toHaveTextContent(LINK_REQUIREMENT)
    expect(screen.getByTestId('t-links-requirement')).toHaveTextContent('send telemetry to FUSION (a local operator)')
  })

  it('draws the options the agents do not report as undetermined and says so, until a box of the group is changed', () => {
    let latest = emptyTelemetry
    const seeded = keepAsInstalled({ ...emptyTelemetry, exportEndpoint: 'x:4317', clusterLinks: true, clusterLinksThroughput: true }, ['clusterLinksOptions', 'clusterLinksHub'])
    render(<Harness initial={seeded} onValue={(v) => (latest = v)} />)
    expect(screen.getByTestId('t-links-kept')).toHaveTextContent(/As installed, not reported/)
    expect(screen.getByTestId('t-links-hub-kept')).toHaveTextContent(/As installed, not reported/)
    for (const id of ['t-links-probe', 't-links-throughput', 't-links-hub']) expect((screen.getByTestId(id) as HTMLInputElement).indeterminate).toBe(true)
    expect(isKept(latest, 'clusterLinksHub')).toBe(true)
    // Deciding the hub makes it the draft's own: the note goes, the box is determined, the options stay as they were.
    fireEvent.click(screen.getByTestId('t-links-hub'))
    expect(screen.queryByTestId('t-links-hub-kept')).toBeNull()
    expect((screen.getByTestId('t-links-hub') as HTMLInputElement).indeterminate).toBe(false)
    expect(latest.clusterLinksHub).toBe(true)
    expect(isKept(latest, 'clusterLinksHub')).toBe(false)
    expect(isKept(latest, 'clusterLinksOptions')).toBe(true)
    expect(screen.getByTestId('t-links-kept')).toBeInTheDocument()
    fireEvent.click(screen.getByTestId('t-links-throughput'))
    expect(screen.queryByTestId('t-links-kept')).toBeNull()
    expect(isKept(latest, 'clusterLinksOptions')).toBe(false)
  })

  it('shows no note and no undetermined box on a draft that states everything', () => {
    render(<Harness onValue={() => {}} />)
    fireEvent.click(screen.getByTestId('t-clusterLinks'))
    expect(screen.queryByTestId('t-links-kept')).toBeNull()
    expect(screen.queryByTestId('t-links-hub-kept')).toBeNull()
    expect((screen.getByTestId('t-links-hub') as HTMLInputElement).indeterminate).toBe(false)
  })
})
