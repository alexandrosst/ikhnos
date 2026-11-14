import { Group, SettingRow } from '@/features/settings/components/SettingsParts'
import { useServer } from '@/store/server'

/** What this server and the agent chart it hands out say they are, and whether it can suggest where a cluster is. */
export default function AboutSettings() {
  const version = useServer((s) => s.info?.version)
  const chart = useServer((s) => s.info?.install?.chartVersion)
  const geoip = useServer((s) => s.info?.geoip)
  if (!version && !chart && !geoip) return null
  return (
    <Group title="About" id="about">
      <div data-testid="about-server" className="divide-y divide-nb-850">
        {version && <SettingRow label="Server" mono value={<span data-testid="about-server-version">{version}</span>} />}
        {chart && <SettingRow label="Agent chart" mono value={<span data-testid="about-chart-version">{chart}</span>} />}
        {geoip && (
          <SettingRow
            label="Location hints"
            tone={geoip.enabled ? 'healthy' : 'off'}
            value={
              geoip.enabled
                ? `${geoip.database || 'Offline GeoIP'}${geoip.builtAt ? `, built ${new Date(geoip.builtAt).toLocaleDateString()}` : ''}`
                : 'Off: no GeoIP database. Region codes and city names in labels still place clusters.'
            }
            valueTestId="about-geoip"
          />
        )}
      </div>
    </Group>
  )
}
