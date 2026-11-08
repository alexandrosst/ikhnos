import { countries } from 'country-flag-icons'
import { AlertTriangle } from 'lucide-react'
import { useMemo, useState } from 'react'
import { EditAction, FormRow, Row, Section, ViewFooter } from '@/components/entity/parts'
import { Place } from '@/components/ui/brand'
import { Button, Input, Modal, Select } from '@/components/ui/primitives'
import { countryName } from '@/lib/present'
import { countryAt, findCities, nearestCity, siteLocationIssue, type City } from '@/lib/places'
import { usePlaceIndex } from '@/lib/places-data'
import { uid, useRawTopology, useTopology } from '@/store/topology'
import { DEFAULT_ORG, SITE_KINDS, type Site } from '@/lib/types'
import { TrustSelect } from './shared'

/* ---------- Site ---------- */
const COUNTRY_OPTIONS = (countries as string[]).map((code) => ({ code, name: countryName(code) })).sort((a, b) => a.name.localeCompare(b.name))

/**
 * A site in a side panel: read-only first (where it is, and whether the country and coordinates agree), then a form. A site is a place you
 * declared, so nothing in it is locked.
 */
export function SiteForm({ initial, onClose, edit, onDelete }: { initial: Site | null; onClose: () => void; edit?: boolean; onDelete?: () => void }) {
  const upsert = useTopology((s) => s.upsertSite)
  const clusters = useTopology((s) => s.clusters)
  const stored = useRawTopology((s) => (initial ? s.sites.find((x) => x.id === initial.id) : undefined))
  const current = stored ?? initial
  const [editing, setEditing] = useState(!initial || !!edit)
  const [f, setF] = useState<Site>(() => current ?? { id: uid('site'), orgId: DEFAULT_ORG, name: '', kind: 'data-center', lat: 0, lng: 0, country: '' })
  const [lat, setLat] = useState(String(f.lat))
  const [lng, setLng] = useState(String(f.lng))
  const latN = Number(lat)
  const lngN = Number(lng)
  const valid = lat.trim() !== '' && lng.trim() !== '' && latN >= -90 && latN <= 90 && lngN >= -180 && lngN <= 180
  // The place tables load when the panel opens: a city search fills everything in, and the coordinates are
  // checked against the country so a typo cannot quietly put a site in the wrong place.
  const places = usePlaceIndex()
  const [search, setSearch] = useState('')
  const hits = useMemo(() => (places ? findCities(places, search, 6) : []), [places, search])
  const here = useMemo(() => {
    if (!places || !valid) return undefined
    const near = nearestCity(places, latN, lngN, 60)
    const cc = countryAt(places, latN, lngN) ?? near?.city.cc
    return { near, cc, issue: siteLocationIssue(places, { lat: latN, lng: lngN, country: f.country }) }
  }, [places, valid, latN, lngN, f.country])
  const pick = (c: City) => {
    setF({ ...f, city: c.name, country: c.cc, name: f.name.trim() ? f.name : c.name })
    setLat(String(c.lat))
    setLng(String(c.lng))
    setSearch('')
  }
  const done = () => (initial ? setEditing(false) : onClose())
  const kindLabel = (k: Site['kind']) => SITE_KINDS.find((x) => x.value === k)?.label ?? k

  /* ---------- read-only ---------- */
  if (!editing && current) {
    const s = current
    const issue = places ? siteLocationIssue(places, s) : undefined
    const here_ = clusters.filter((c) => c.siteId === s.id).map((c) => c.name)
    return (
      <Modal
        open
        side
        onClose={onClose}
        title={s.name}
        description={issue ? <p className="flex items-center gap-1.5 text-xs text-warn" title={issue}><AlertTriangle size={12} aria-hidden /> Country and coordinates disagree</p> : undefined}
        headerActions={<EditAction onClick={() => { setF(s); setLat(String(s.lat)); setLng(String(s.lng)); setEditing(true) }} />}
        footer={<ViewFooter onDelete={onDelete} legend={false} />}
      >
        <dl>
          <Section title="Identity">
            <Row label="Name">{s.name}</Row>
            <Row label="Kind">{kindLabel(s.kind)}</Row>
          </Section>
          <Section title="Location">
            <Row label="Place">{s.city || s.country ? <Place site={s} /> : undefined}</Row>
            <Row label="Coordinates" mono>{`${s.lat.toFixed(4)}, ${s.lng.toFixed(4)}`}</Row>
          </Section>
          <Section title="Policy">
            <Row label="Trust zone">{s.trustZone && s.trustZone[0].toUpperCase() + s.trustZone.slice(1)}</Row>
            <Row label="Data residency">{s.dataResidency}</Row>
          </Section>
          <Section title="Clusters">
            <Row label="Clusters">{here_.length > 0 ? here_.join(', ') : undefined}</Row>
          </Section>
        </dl>
      </Modal>
    )
  }

  /* ---------- form ---------- */
  return (
    <Modal
      open
      side
      onClose={onClose}
      title={initial ? `Edit ${current?.name ?? 'site'}` : 'Add site'}
      description={initial ? undefined : 'A physical place. Clusters are attached to a site; the map view will draw sites as dots.'}
      footer={
        <>
          <Button onClick={done}>Cancel</Button>
          <Button
            variant="primary"
            disabled={!f.name.trim() || !valid}
            onClick={() => {
              upsert({ ...f, name: f.name.trim(), city: f.city?.trim() || undefined, lat: latN, lng: lngN, country: f.country.trim().toUpperCase() })
              done()
            }}
          >
            {initial ? 'Save changes' : 'Add'}
          </Button>
        </>
      }
    >
      <FormRow label="Name">
        <Input autoFocus value={f.name} onChange={(e) => setF({ ...f, name: e.target.value })} placeholder="e.g. Patras edge site" />
      </FormRow>
      <FormRow label="Find a city" hint="Fills in city, country and coordinates from a table of ~34,000 cities." group>
        <Input value={search} onChange={(e) => setSearch(e.target.value)} placeholder={places ? 'Type a city name…' : 'Loading the city table…'} disabled={!places} aria-label="Find a city" />
        {hits.length > 0 && (
          <ul className="mt-1 overflow-hidden rounded-md border border-nb-800 bg-nb-930" role="listbox" aria-label="Matching cities">
            {hits.map((c) => (
              <li key={`${c.name}-${c.cc}-${c.lat}-${c.lng}`}>
                <button type="button" role="option" aria-selected={false} className="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-sm text-nb-300 hover:bg-nb-850 hover:text-nb-300" onClick={() => pick(c)}>
                  <span>{c.name}, <span className="text-nb-500">{countryName(c.cc)}</span></span>
                  <span className="text-xs text-nb-600">{c.pop >= 1000 ? `${Math.round(c.pop / 1000).toLocaleString()}k people` : ''}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </FormRow>
      <FormRow label="Kind">
        <Select value={f.kind} onChange={(e) => setF({ ...f, kind: e.target.value as Site['kind'] })}>
          {SITE_KINDS.map((k) => (
            <option key={k.value} value={k.value}>
              {k.label}
            </option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="City">
        <Input value={f.city ?? ''} onChange={(e) => setF({ ...f, city: e.target.value || undefined })} placeholder="e.g. Patras" />
      </FormRow>
      <FormRow label="Country">
        <Select value={f.country} onChange={(e) => setF({ ...f, country: e.target.value })}>
          <option value="">Not set</option>
          {f.country && !COUNTRY_OPTIONS.some((c) => c.code === f.country) && <option value={f.country}>{f.country}</option>}
          {COUNTRY_OPTIONS.map((c) => (
            <option key={c.code} value={c.code}>{c.name}</option>
          ))}
        </Select>
      </FormRow>
      <FormRow label="Latitude" hint="-90 to 90">
        <Input inputMode="decimal" value={lat} onChange={(e) => setLat(e.target.value)} />
      </FormRow>
      <FormRow label="Longitude" hint="-180 to 180">
        <Input inputMode="decimal" value={lng} onChange={(e) => setLng(e.target.value)} />
      </FormRow>
      <FormRow label="Data residency" hint="Jurisdiction, e.g. EU.">
        <Input value={f.dataResidency ?? ''} onChange={(e) => setF({ ...f, dataResidency: e.target.value || undefined })} />
      </FormRow>
      <FormRow label="Trust zone">
        <TrustSelect value={f.trustZone} onChange={(v) => setF({ ...f, trustZone: v })} />
      </FormRow>
      {!valid && <p className="mt-2 text-xs text-bad">Enter a latitude between -90 and 90 and a longitude between -180 and 180.</p>}
      {here?.issue && (
        <p className="mt-2 flex flex-wrap items-center gap-2 rounded-md border border-warn/30 bg-warn/10 px-3 py-2 text-xs text-warn" role="alert">
          {here.issue}
          {here.cc && <button type="button" className="rounded border border-warn/40 px-1.5 py-0.5 hover:bg-warn/10" onClick={() => setF({ ...f, country: here.cc! })}>Set country to {countryName(here.cc)}</button>}
        </p>
      )}
      {here && !here.issue && (!f.country || !f.city) && (here.cc || here.near) && (
        <p className="mt-2 flex flex-wrap items-center gap-2 text-xs text-nb-400">
          These coordinates are {here.near ? `near ${here.near.city.name}, ` : 'in '}{countryName(here.near?.city.cc ?? here.cc)}.
          <button type="button" className="rounded border border-accent/40 px-1.5 py-0.5 text-accent hover:bg-accent/10" onClick={() => setF({ ...f, country: f.country || (here.cc ?? ''), city: f.city || here.near?.city.name })}>Fill in the blanks</button>
        </p>
      )}
    </Modal>
  )
}
