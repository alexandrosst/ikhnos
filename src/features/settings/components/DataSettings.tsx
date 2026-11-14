import { Download, RotateCcw, Trash2, Upload } from 'lucide-react'
import { useRef, useState } from 'react'
import { ConfirmModal } from '@/components/forms'
import { Button, ICON_SM, SavedNote } from '@/components/ui/primitives'
import { Group, SettingRow } from '@/features/settings/components/SettingsParts'
import { atLeast } from '@/lib/api'
import { declaredNote, rehydrate, toDeclared } from '@/lib/declared'
import { useServer } from '@/store/server'
import { exportTopology, normalize, useRawTopology, useTopology } from '@/store/topology'

/** The topology as a file: take a copy, bring one in, start over. */
export default function DataSettings() {
  const onServer = useServer((s) => s.status === 'connected' && atLeast(s.role, 'admin'))
  const connected = useServer((s) => s.status === 'connected')
  const geoip = useServer((s) => s.info?.geoip)
  const { replaceAll, reset, clear, clusters, nodes, services, devices, dependencies } = useTopology()
  const fileRef = useRef<HTMLInputElement>(null)
  const [msg, setMsg] = useState<{ ok: boolean; text: string } | null>(null)
  const [confirm, setConfirm] = useState<'clear' | 'reset' | null>(null)

  const download = () => {
    // A file holds what people declared. What agents observed is not exported: it is the server's to report, and a
    // copy in a file would come back later as if it were still true.
    const current = exportTopology()
    const { model, report } = toDeclared(current)
    const blob = new Blob([JSON.stringify({ ...model, schemaVersion: current.schemaVersion }, null, 2)], { type: 'application/json' })
    const a = document.createElement('a')
    a.href = URL.createObjectURL(blob)
    a.download = 'topology.json'
    a.click()
    URL.revokeObjectURL(a.href)
    const note = declaredNote(report)
    setMsg(note ? { ok: true, text: `Exported the declared workspace. ${note.replace(/^Removed/, 'Left out')}` } : null)
  }

  const onFile = async (f: File | undefined) => {
    if (!f) return
    try {
      // normalize refuses a file from a newer version with a message saying so. What it accepts is reduced to what
      // people declared: an older file may carry discovered records, which are not brought in.
      const { model: t, report } = toDeclared(normalize(JSON.parse(await f.text())))
      // Signed in, the records this browser already holds from the server stay; on their own the imported file is all there is.
      replaceAll(connected ? rehydrate(t, useRawTopology.getState()) : t)
      const note = declaredNote(report)
      setMsg({ ok: true, text: `${note ? note + ' ' : ''}Imported ${t.clusters.length} clusters, ${t.nodes.length} nodes, ${t.services.length} services, ${t.devices.length} devices, ${t.dependencies.length} dependencies, ${t.applications.length} applications, ${t.sites.length} sites.` })
    } catch (e) {
      setMsg({ ok: false, text: e instanceof Error ? e.message : 'Could not read file.' })
    }
    if (fileRef.current) fileRef.current.value = ''
  }

  return (
    <div className="space-y-6" data-testid="settings-data">
      <p className="ml-0.5 text-sm text-nb-400" data-testid="settings-counts">
        {[
          [clusters.length, 'cluster'],
          [nodes.length, 'node'],
          [services.length, 'service'],
          [devices.length, 'device'],
          [dependencies.length, 'dependenc'],
        ].map(([n, w], i) => (
          <span key={String(w)}>
            {i > 0 && ' · '}
            <span className="font-medium text-nb-300">{n}</span> {w === 'dependenc' ? (n === 1 ? 'dependency' : 'dependencies') : `${w}${n === 1 ? '' : 's'}`}
          </span>
        ))}
      </p>
      <Group title="Your topology" id="data" note={connected ? (onServer ? 'Saved on the server and shared with everyone who signs in.' : 'Saved on the server.') : 'Stored in this browser.'}>
        <SettingRow label="Export" value="Download as JSON" action={<Button size="sm" onClick={download} data-testid="export-json"><Download size={ICON_SM} /> Export</Button>} />
        <SettingRow
          label="Import"
          value="Replace it from a JSON file"
          action={
            <>
              <Button size="sm" onClick={() => fileRef.current?.click()} data-testid="import-json"><Upload size={ICON_SM} /> Import</Button>
              <input ref={fileRef} type="file" accept="application/json,.json" hidden onChange={(e) => onFile(e.target.files?.[0])} data-testid="import-file" />
            </>
          }
        />
        <SettingRow label="Sample" value="The cloud → edge → far-edge example" action={<Button size="sm" onClick={() => setConfirm('reset')} data-testid="load-sample"><RotateCcw size={ICON_SM} /> Load</Button>} />
      </Group>
      {msg && <SavedNote className="block" tone={msg.ok ? 'ok' : 'error'}>{msg.text}</SavedNote>}
      <Group title="Danger" id="danger">
        <SettingRow label="Clear everything" value="Start from a blank canvas" action={<Button size="sm" variant="danger" onClick={() => setConfirm('clear')} data-testid="clear-all"><Trash2 size={ICON_SM} /> Clear all</Button>} />
      </Group>
      <p className="ml-0.5 text-xs text-nb-500">
        Place data: city names and coordinates from <a className="underline hover:text-nb-400" href="https://www.geonames.org/" target="_blank" rel="noreferrer">GeoNames</a> (CC BY 4.0), country outlines from Natural Earth.{' '}
        {geoip?.enabled && geoip.attribution ? `${geoip.attribution}.` : <>When the server has a GeoIP database, IP geolocation by <a className="underline hover:text-nb-400" href="https://db-ip.com" target="_blank" rel="noreferrer">DB-IP.com</a> (CC BY 4.0) or MaxMind GeoLite2.</>}
      </p>
      {confirm && (
        <ConfirmModal
          title={confirm === 'clear' ? 'Clear the whole topology?' : 'Replace with the sample topology?'}
          message={onServer ? 'This replaces the shared workspace on the server for everyone. Export first if you want a backup.' : 'This replaces what is currently stored in this browser. Export first if you want a backup.'}
          confirmLabel={confirm === 'clear' ? 'Clear' : 'Replace'}
          onConfirm={confirm === 'clear' ? clear : reset}
          onClose={() => setConfirm(null)}
        />
      )}
    </div>
  )
}
