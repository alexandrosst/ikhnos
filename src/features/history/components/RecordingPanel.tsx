import { ChevronRight, CircleAlert } from 'lucide-react'
import { useEffect, useState } from 'react'
import { Button, Field, ICON_SM, Input, Modal, SavedNote, Segmented, Select } from '@/components/ui/primitives'
import type { Conn } from '@/lib/api'
import type { AppSettings } from '@/lib/history'
import { useSettings } from '@/store/settings'

// The three things people reason about when they decide how much history to keep, as choices instead of free numbers. A value saved earlier
// (through the API, or before these choices existed) that none of them matches is kept as a choice of its own, so opening the panel never changes it.
const EVERY = [1, 5, 15, 30, 60]
const KEEP = [7, 30, 90, 365]
const SIZE = [256, 512, 1024, 4096, 8192]
const LOG = [0, 30, 90, 365]

const withCurrent = (options: number[], current: number) => (options.includes(current) ? options : [...options, current].sort((a, b) => a - b))
const minutes = (n: number) => (n === 60 ? 'hour' : n % 60 === 0 && n > 60 ? `${n / 60} hours` : n === 1 ? 'minute' : `${n} minutes`)
const days = (n: number) => (n === 365 ? '1 y' : `${n} d`)
const size = (mb: number) => (mb >= 1024 && mb % 1024 === 0 ? `${mb / 1024} GB` : `${mb} MB`)
const logKept = (n: number) => (n === 0 ? 'Forever' : n === 365 ? 'A year' : `${n} days`)

// flowStaleSeconds is stored in plain seconds; the field lets someone type it in whichever unit is convenient (seconds through days).
const FLOW_STALE_UNITS = [
  { label: 'seconds', seconds: 1 },
  { label: 'minutes', seconds: 60 },
  { label: 'hours', seconds: 3600 },
  { label: 'days', seconds: 86400 },
]
const FLOW_STALE_MIN_SECONDS = 1
const FLOW_STALE_MAX_SECONDS = 30 * 86400
const STALE_BEATS = { min: 2, max: 20 }

/** The largest unit that divides the stored seconds evenly, so the field starts out showing a whole number (300 s as "5 minutes"). */
function bestFlowStaleUnit(seconds: number): number {
  for (const u of [...FLOW_STALE_UNITS].reverse()) {
    if (u.seconds <= seconds && seconds % u.seconds === 0) return u.seconds
  }
  return 1
}

function Row({ label, hint, children }: { label: string; hint: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-5 border-b border-nb-850 py-4 first:pt-0">
      <div className="min-w-0">
        <div className="text-sm font-medium text-nb-300">{label}</div>
        <p className="mt-1 max-w-[16rem] text-xs leading-relaxed text-nb-500">{hint}</p>
      </div>
      <div className="shrink-0">{children}</div>
    </div>
  )
}

/** How the landscape is recorded and kept. A side panel opened from the History page; the settings are the server's, so they reach every agent. */
export default function RecordingPanel({ admin, conn, onClose }: { admin: boolean; conn: Conn; onClose: () => void }) {
  const { settings, save, error, loaded } = useSettings()
  const [every, setEvery] = useState(settings.snapshotMinutes)
  const [keep, setKeep] = useState(settings.retentionDays)
  const [limit, setLimit] = useState(settings.maxHistoryMb)
  const [log, setLog] = useState(settings.eventRetentionDays)
  const [beats, setBeats] = useState(String(settings.staleAfterBeats))
  const [unit, setUnit] = useState(() => bestFlowStaleUnit(settings.flowStaleSeconds))
  const [quiet, setQuiet] = useState(() => String(settings.flowStaleSeconds / bestFlowStaleUnit(settings.flowStaleSeconds)))
  const [busy, setBusy] = useState(false)
  const [saved, setSaved] = useState(false)
  // Settings load after the page; when they arrive (or change under us) the panel starts from them, as it did when it was a form on the page.
  useEffect(() => {
    setEvery(settings.snapshotMinutes)
    setKeep(settings.retentionDays)
    setLimit(settings.maxHistoryMb)
    setLog(settings.eventRetentionDays)
    setBeats(String(settings.staleAfterBeats))
    const u = bestFlowStaleUnit(settings.flowStaleSeconds)
    setUnit(u)
    setQuiet(String(settings.flowStaleSeconds / u))
  }, [settings])

  const beatsNum = Number(beats)
  const beatsValid = Number.isInteger(beatsNum) && beatsNum >= STALE_BEATS.min && beatsNum <= STALE_BEATS.max
  const quietNum = Number(quiet)
  const quietSeconds = Math.round(quietNum * unit)
  const quietValid = Number.isFinite(quietNum) && quietNum > 0 && quietSeconds >= FLOW_STALE_MIN_SECONDS && quietSeconds <= FLOW_STALE_MAX_SECONDS
  const next: Partial<AppSettings> = { snapshotMinutes: every, retentionDays: keep, maxHistoryMb: limit, eventRetentionDays: log, staleAfterBeats: beatsNum, flowStaleSeconds: quietSeconds }
  const dirty = (Object.keys(next) as (keyof AppSettings)[]).some((k) => settings[k] !== next[k])
  const touch = () => setSaved(false)

  return (
    <Modal
      open
      side
      width="max-w-md"
      onClose={onClose}
      title="Recording"
      description="How the landscape is recorded and kept"
      footer={
        <>
          {saved && <SavedNote className="mr-auto">Saved. Agents were told.</SavedNote>}
          <Button onClick={onClose}>{saved ? 'Close' : 'Cancel'}</Button>
          {admin && (
            <Button
              variant="primary"
              disabled={!dirty || busy || !loaded || !beatsValid || !quietValid}
              onClick={async () => {
                setBusy(true)
                setSaved(await save(conn, { ...settings, ...next } as AppSettings))
                setBusy(false)
              }}
              data-testid="save-settings"
            >
              Save
            </Button>
          )}
        </>
      }
    >
      <Row label="Record every" hint="Also right after a change, and only when something differs.">
        <Select value={String(every)} onChange={(e) => { touch(); setEvery(Number(e.target.value)) }} disabled={!admin} aria-label="Record every" className="w-36" data-testid="setting-snapshotMinutes">
          {withCurrent(EVERY, every).map((n) => <option key={n} value={n}>{minutes(n)}</option>)}
        </Select>
      </Row>
      <Row label="Keep for" hint="A day of every recording, then one an hour for a week, then one a day.">
        <Segmented label="Keep for" value={keep} disabled={!admin} onChange={(v) => { touch(); setKeep(v) }} options={withCurrent(KEEP, keep).map((n) => ({ value: n, label: days(n) }))} />
      </Row>
      <Row label="Size limit" hint="The oldest recordings go first when it is passed. The newest is always kept.">
        <Select value={String(limit)} onChange={(e) => { touch(); setLimit(Number(e.target.value)) }} disabled={!admin} aria-label="Size limit" className="w-36" data-testid="setting-maxHistoryMb">
          {withCurrent(SIZE, limit).map((n) => <option key={n} value={n}>{size(n)}</option>)}
        </Select>
      </Row>
      <Row label="Keep the change log" hint="The list of changes only. What people did is kept forever.">
        <Select value={String(log)} onChange={(e) => { touch(); setLog(Number(e.target.value)) }} disabled={!admin} aria-label="Keep the change log" className="w-36" data-testid="setting-eventRetentionDays">
          {withCurrent(LOG, log).map((n) => <option key={n} value={n}>{logKept(n)}</option>)}
        </Select>
      </Row>

      <details className="group mt-4" data-testid="recording-advanced">
        <summary className="flex cursor-pointer select-none items-center gap-1 text-xs text-nb-500 marker:content-none">
          <ChevronRight size={ICON_SM} className="transition-transform group-open:rotate-90" aria-hidden />
          Freshness, for slow links
        </summary>
        <div className="mt-3 space-y-4">
          <Field label="Mark an agent stale after" hint={beatsValid ? 'An agent beats every 30 seconds. Its records show as stale once this many beats are missed.' : `Between ${STALE_BEATS.min} and ${STALE_BEATS.max}.`} hintId="setting-staleAfterBeats-hint">
            <div className="flex items-center gap-2">
              <Input type="number" inputMode="numeric" min={STALE_BEATS.min} max={STALE_BEATS.max} value={beats} disabled={!admin} aria-invalid={!beatsValid} aria-describedby="setting-staleAfterBeats-hint" onChange={(e) => { touch(); setBeats(e.target.value) }} className="w-24" data-testid="setting-staleAfterBeats" />
              <span className="text-xs text-nb-500">missed beats</span>
            </div>
          </Field>
          <Field label="Link is quiet after" hint={quietValid ? 'How long an observed link may go unseen before it is shown as quiet.' : 'Between 1 second and 30 days.'} hintId="setting-flowStaleSeconds-hint">
            <div className="flex items-center gap-2">
              <Input type="number" inputMode="decimal" value={quiet} disabled={!admin} aria-invalid={!quietValid} aria-describedby="setting-flowStaleSeconds-hint" onChange={(e) => { touch(); setQuiet(e.target.value) }} className="w-24" data-testid="setting-flowStaleSeconds" />
              <select
                className="h-9 rounded-md border border-nb-800 bg-nb-925 px-2 text-sm text-nb-300 disabled:cursor-not-allowed disabled:opacity-50"
                value={unit}
                disabled={!admin}
                onChange={(e) => {
                  touch()
                  const nextUnit = Number(e.target.value)
                  const cur = Number.isFinite(quietNum) ? quietNum * unit : settings.flowStaleSeconds
                  setUnit(nextUnit)
                  setQuiet(String(Math.round(cur / nextUnit)))
                }}
                data-testid="setting-flowStaleUnit"
              >
                {FLOW_STALE_UNITS.map((u) => <option key={u.label} value={u.seconds}>{u.label}</option>)}
              </select>
            </div>
          </Field>
        </div>
      </details>

      {error && <p className="mt-4 text-sm text-bad" role="alert"><CircleAlert size={ICON_SM} className="mr-1 inline" aria-hidden />{error}</p>}
      <p className="mt-5 text-xs text-nb-500">{admin ? 'Applies to every connected agent within a minute.' : 'Only administrators can change these.'}</p>
    </Modal>
  )
}
