import { useState } from 'react'
import { CopyCommand } from '@/components/ui/CopyCommand'
import { Button, Field, InfoTip, Input } from '@/components/ui/primitives'
import { Group, RowMore, SettingRow } from '@/features/settings/components/SettingsParts'
import { exposureHelp, exposureIsAddressSwap, exposureLabel, serverAddressUpgradeCommand } from '@/features/settings/lib/exposure'
import { useServer } from '@/store/server'

/**
 * What every agent dials to reach this server, and how it is exposed. Read-only: this page cannot run helm for you. Where the address can
 * be swapped, Change opens a field and the exact upgrade command, so nobody has to rebuild it from three pages of docs.
 */
export default function ServerAddressSettings() {
  const info = useServer((s) => s.info)
  const [changing, setChanging] = useState(false)
  const [next, setNext] = useState('')
  const address = info?.agentAddress ?? ''
  const kind = info?.agentExposure ?? ''
  const swappable = exposureIsAddressSwap(kind)

  return (
    <Group title="Address" id="server-address">
      <div data-testid="server-address-settings" className="divide-y divide-nb-850">
        <SettingRow
          label="Agents dial"
          mono
          value={address || '(not set)'}
          valueTestId="server-address"
          action={swappable && <Button size="sm" onClick={() => setChanging((c) => !c)} aria-expanded={changing} data-testid="server-address-change">Change</Button>}
        />
        <SettingRow
          label="Exposed via"
          value={
            <>
              {exposureLabel(kind)}
              {swappable && <InfoTip>{exposureHelp(kind)}</InfoTip>}
            </>
          }
          valueTestId="server-exposure"
        />
        {!swappable && (
          <RowMore testId="server-exposure-help">
            <p className="text-xs text-nb-400">{exposureHelp(kind)}</p>
          </RowMore>
        )}
        {swappable && changing && (
          <RowMore>
            <Field label="New address" hint="host:port. Nothing changes until you run the command.">
              <Input
                value={next}
                onChange={(e) => setNext(e.target.value)}
                placeholder={kind === 'nodeport' ? 'NODE_IP:30443' : 'EXTERNAL_IP:8443'}
                spellCheck={false}
                autoCapitalize="none"
                autoCorrect="off"
                autoComplete="off"
                className="font-mono text-[13px]"
                data-testid="server-address-input"
              />
            </Field>
            <CopyCommand text={serverAddressUpgradeCommand(info, next.trim())} />
            {/* Helm cannot recall the chart an install used, so <chart> is always the person's to fill in; the release and namespace only when this server could not say. */}
            <p className="text-xs text-nb-500">
              Replace the <span className="font-mono">&lt;angle brackets&gt;</span>. Needs Helm 3.14 or later.
            </p>
          </RowMore>
        )}
      </div>
    </Group>
  )
}
