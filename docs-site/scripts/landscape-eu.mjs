// Writes the landscape the videos and screenshots show: the built-in sample, moved to Berlin, Athens and Paris, so the picture is a
// European one rather than a Greek one. It is the whole workspace as the browser stores it (key ikhnos/v1), so a recording starts with it in
// place. It is made from the sample itself, so the two never drift.
//
//   node scripts/landscape-eu.mjs        # from docs-site/; writes scripts/landscape-eu.json; load it with localStorage.setItem('ikhnos/v1', JSON.stringify({state: it, version: it.schemaVersion}))
import {build} from 'esbuild';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath, pathToFileURL} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..');
const out = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'ikh-seed-')), 'seed.mjs');
await build({entryPoints: [path.join(repo, 'src', 'lib', 'seed.ts')], bundle: true, format: 'esm', outfile: out, logLevel: 'silent', alias: {'@': path.join(repo, 'src')}});
const {seedTopology} = await import(pathToFileURL(out).href);
const m = seedTopology();

const rename = (s) => s
  .replace(/aws-eu-central/g, 'berlin-cloud').replace(/athens-regional/g, 'athens-regional')
  .replace(/edge-patras/g, 'paris-edge').replace(/patras-/g, 'paris-').replace(/Patras/g, 'Paris')
  .replace(/edge-thessaloniki/g, 'athens-lab').replace(/thess-/g, 'lab-').replace(/Thessaloniki/g, 'Athens lab');
const walk = (v) => (typeof v === 'string' ? rename(v) : Array.isArray(v) ? v.map(walk) : v && typeof v === 'object' ? Object.fromEntries(Object.entries(v).map(([k, x]) => [k, walk(x)])) : v);

const model = walk(m);
for (const c of model.clusters) {
  if (c.id === 'cl-cloud') Object.assign(c, {distribution: 'RKE2', provider: 'IONOS', region: 'Berlin', siteId: 'site-ber', cni: 'cilium', ingress: 'ingress-nginx', podCidr: '10.0.0.0/16', serviceCidr: '10.43.0.0/16', cloudAccount: undefined, apiEndpoint: undefined});
  if (c.id === 'cl-edge-a') Object.assign(c, {siteId: 'site-par', region: 'Paris', egressIp: undefined});
  if (c.id === 'cl-edge-b') Object.assign(c, {siteId: 'site-lab', region: 'Athens lab', egressIp: undefined});
}
for (const n of model.nodes) {
  if (n.clusterId === 'cl-cloud') Object.assign(n, {name: n.name.replace('eks-', 'ber-'), instanceType: undefined, zone: n.zone ? 'berlin-1' + n.zone.slice(-1) : undefined});
}
for (const d of model.devices) d.siteId = {'site-pat': 'site-par', 'site-thess': 'site-lab'}[d.siteId] ?? d.siteId;
model.sites = [
  {id: 'site-ber', orgId: m.sites[0].orgId, name: 'Berlin', kind: 'cloud-region', lat: 52.52, lng: 13.405, country: 'DE', city: 'Berlin', dataResidency: 'EU'},
  m.sites[1],
  {id: 'site-par', orgId: m.sites[0].orgId, name: 'Paris edge site', kind: 'edge-site', lat: 48.8566, lng: 2.3522, country: 'FR', city: 'Paris', dataResidency: 'EU', trustZone: 'restricted'},
  {id: 'site-lab', orgId: m.sites[0].orgId, name: 'Athens lab', kind: 'edge-site', lat: 38.02, lng: 23.82, country: 'GR', city: 'Athens', dataResidency: 'EU', trustZone: 'restricted'},
];
const l = (id, a, b, rtt, loss, mbps) => ({...m.siteLinks[0], id, a, b, rttMs: rtt, lossPct: loss, mbps});
model.siteLinks = [l('sl1', 'site-ber', 'site-ath', 38, 0.1, 200), l('sl2', 'site-ath', 'site-par', 49, 0.2, 100), l('sl3', 'site-ath', 'site-lab', 4, 0.1, 1000), l('sl4', 'site-ber', 'site-par', 16, 0.1, 200)];
model.agents = model.agents.filter((a) => a.id !== 'ag-volos');
model.suggestions = model.suggestions.filter((x) => x.id !== 'sg-volos');
model.applications = model.applications.map((a) => ({...a, description: a.description.replace('across the ikhnos', 'across the continuum')}));
const schemaVersion = Number(fs.readFileSync(path.join(repo, 'src', 'lib', 'types.ts'), 'utf8').match(/SCHEMA_VERSION = (\d+)/)[1]);
const json = JSON.stringify({...model, schemaVersion}, (_k, v) => (v === undefined ? undefined : v), 2);
fs.writeFileSync(path.join(here, 'landscape-eu.json'), json);
console.log('wrote landscape-eu.json:', model.clusters.map((c) => `${c.name} (${c.region})`).join(', '));
