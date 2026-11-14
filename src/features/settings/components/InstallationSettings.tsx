import clsx from 'clsx'
import { CircleAlert, Save } from 'lucide-react'
import { useId, useState } from 'react'
import { Button, ICON_SM, Input, SavedNote } from '@/components/ui/primitives'
import { Group, RowMore, SettingRow } from '@/features/settings/components/SettingsParts'
import { atLeast, type Conn } from '@/lib/api'
import { imageOk, imageProblems, previewImage } from '@/lib/image'
import { useServer } from '@/store/server'
import { useSettings } from '@/store/settings'

/** One labelled input. The label, the help and the problem are tied to the input, so a screen reader hears all three. */
function TextField({ label, help, problem, value, onChange, disabled, placeholder, testId }: { label: string; help: React.ReactNode; problem: string; value: string; onChange: (v: string) => void; disabled: boolean; placeholder: string; testId: string }) {
  const id = useId()
  return (
    <div>
      <label htmlFor={id} className="mb-1.5 block text-sm font-medium text-nb-300">{label}</label>
      <Input
        id={id}
        value={value}
        disabled={disabled}
        placeholder={placeholder}
        spellCheck={false}
        autoCapitalize="none"
        autoCorrect="off"
        autoComplete="off"
        aria-invalid={!!problem}
        aria-describedby={`${id}-help ${id}-problem`}
        onChange={(e) => onChange(e.target.value)}
        className={clsx('font-mono text-[13px]', problem && 'border-bad/60')}
        data-testid={testId}
      />
      <p id={`${id}-problem`} role={problem ? 'alert' : undefined} className={clsx('mt-1 text-xs text-bad', !problem && 'hidden')} data-testid={`${testId}-problem`}>{problem}</p>
      <p id={`${id}-help`} className="mt-1 text-xs text-nb-500">{help}</p>
    </div>
  )
}

const Code = ({ children }: { children: React.ReactNode }) => <code className="font-mono text-nb-300">{children}</code>

/**
 * Where install commands pull the agent image and chart from. Nothing is preset: an administrator points this
 * organisation's commands at a registry they published to (see scripts/publish.sh), optionally pinned by digest.
 */
export default function InstallationSettings({ conn }: { conn: Conn }) {
  const admin = useServer((s) => atLeast(s.role, 'admin'))
  const reloadInfo = useServer((s) => s.reloadInfo)
  const chartVersion = useServer((s) => s.info?.install?.chartVersion ?? '')
  const { settings, save, error, loaded } = useSettings()
  const [draft, setDraft] = useState({ registry: '', tag: '', digest: '' })
  const [saved, setSaved] = useState(false)
  const [editing, setEditing] = useState(false)
  const [busy, setBusy] = useState(false)
  // Follow the server's values when they change (loaded, saved, another tab), without an effect.
  const [seen, setSeen] = useState(settings)
  if (seen !== settings) {
    setSeen(settings)
    setDraft({ registry: settings.imageRegistry, tag: settings.imageTag, digest: settings.imageDigest })
  }

  const problems = imageProblems(draft)
  const ok = imageOk(problems)
  const dirty = draft.registry.trim() !== settings.imageRegistry || draft.tag.trim() !== settings.imageTag || draft.digest.trim() !== settings.imageDigest
  const preview = previewImage(draft, settings.imageDefaults)
  const set = (k: 'registry' | 'tag' | 'digest') => (v: string) => {
    setSaved(false)
    setDraft((d) => ({ ...d, [k]: v }))
  }
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!ok || !dirty || busy) return
    setBusy(true)
    const done = await save(conn, { ...settings, imageRegistry: draft.registry.trim().replace(/\/+$/, ''), imageTag: draft.tag.trim(), imageDigest: draft.digest.trim() })
    if (done) await reloadInfo() // the install wizard reads what the server now resolves
    setSaved(done)
    setBusy(false)
  }

  const imageValue = !preview.configured ? (
    <span data-testid="preview-none">The chart&rsquo;s built-in name, <Code>{preview.repository}</Code></span>
  ) : preview.digest ? (
    <span data-testid="preview-image-text">{preview.pinnedReference}</span>
  ) : (
    <span data-testid="preview-image-text">{preview.reference}{!preview.tag && <span className="font-sans text-nb-500"> (the chart&rsquo;s own version)</span>}</span>
  )
  const pinned = ok && preview.configured && !!preview.digest

  return (
    <Group title="Where agents come from" id="installation">
      <div data-testid="installation-settings" className="divide-y divide-nb-850">
        <SettingRow
          label="Image"
          mono
          tone={!ok ? 'attention' : pinned ? 'healthy' : preview.configured ? 'attention' : 'off'}
          value={ok ? <span data-testid="preview-image">{imageValue}{preview.fromServer && <span className="font-sans text-nb-500"> · server default</span>}</span> : <span className="font-sans" data-testid="preview-invalid">Fix the highlighted fields</span>}
          action={admin && <Button size="sm" onClick={() => setEditing((e) => !e)} aria-expanded={editing} data-testid="installation-edit">{editing ? 'Close' : 'Edit'}</Button>}
        />
        {ok && preview.configured && <SettingRow label="Chart" mono value={<span data-testid="preview-chart">{preview.chartRef}{chartVersion && !dirty && <span className="font-sans text-nb-500"> · version {chartVersion}</span>}</span>} />}
        {ok && preview.configured && (
          <SettingRow label="Pinned" value={<span data-testid="preview-pin">{pinned ? 'Yes. Every install pulls exactly this image.' : 'No. A tag can move, so pin a digest.'}</span>} />
        )}
        {!admin && <SettingRow label="Changing" value="Only administrators can change these." />}
        {admin && editing && (
          <RowMore>
            <form onSubmit={submit} noValidate className="space-y-4">
              <div className="grid gap-4 md:grid-cols-2">
                <TextField
                  label="Image registry"
                  value={draft.registry}
                  onChange={set('registry')}
                  problem={problems.registry}
                  disabled={!admin}
                  placeholder="registry.example.com/team"
                  testId="image-registry"
                  help={<>Host and optional path, no <Code>https://</Code>. A bare name is a Docker Hub namespace.</>}
                />
                <TextField
                  label="Image tag"
                  value={draft.tag}
                  onChange={set('tag')}
                  problem={problems.tag}
                  disabled={!admin}
                  placeholder="chart’s own version"
                  testId="image-tag"
                  help="Optional. Empty uses the chart’s own version."
                />
                <div className="md:col-span-2">
                  <TextField
                    label="Image digest"
                    value={draft.digest}
                    onChange={set('digest')}
                    problem={problems.digest}
                    disabled={!admin}
                    placeholder="sha256:…"
                    testId="image-digest"
                    help={<>Optional, and safest: <Code>sha256:</Code> and 64 hex digits pin the image, and the tag is then ignored.</>}
                  />
                </div>
              </div>
              {error && (
                <p className="text-sm text-bad" role="alert" data-testid="image-error">
                  <CircleAlert size={ICON_SM} className="mr-1 inline" aria-hidden />
                  {error}
                </p>
              )}
              <div className="flex flex-wrap items-center gap-3">
                <Button type="submit" variant="primary" disabled={!dirty || !ok || !loaded || busy} data-testid="save-image">
                  <Save size={ICON_SM} /> {busy ? 'Saving…' : 'Save'}
                </Button>
                {saved && !dirty && <SavedNote data-testid="image-saved">Saved. New install commands use these values.</SavedNote>}
              </div>
            </form>
          </RowMore>
        )}
      </div>
    </Group>
  )
}
