// Records the short screen videos the documentation shows, from the real UI, and writes them to static/video as MP4.
//
//   node scripts/record-videos.mjs [name ...]      # from docs-site/; no name records the tour. The UI runs at IKHNOS_URL, default http://localhost:5173 (npm run dev, repo root)
//
// Each video is a script of steps: move the pointer to something, click it, say what is on screen in a caption, wait. There is no
// narration, so the captions are the explanation and the videos work with the sound off. Re-run after the UI changes.
//
// The tour and the topology video run on the sample landscape, moved to Berlin, Athens and Paris (scripts/landscape-eu.mjs). The others need a real server, so they run against IKHNOS_URL, in this order, on an
// empty one (sign-in, connect-a-cluster, approve), with these set:
//   IKHNOS_FIRST_PASSWORD   the one-time admin password the server printed at its first start (sign-in)
//   IKHNOS_ADMIN_PASSWORD   the password the sign-in video chooses, which later videos sign in with (default below)
//   IKHNOS_AGENT_CMD        a shell command that starts an agent for the cluster to connect; it gets the install command's pin and
//                           token as $PIN and $TOKEN (connect-a-cluster)
//   IKHNOS_AGENT_LOG        the file that agent writes its log to, for the approval code (approve)
//   IKHNOS_APPLY_TELEMETRY_CMD   a shell command that restarts that agent as the chart would once the telemetry commands were run, with the
//                           telemetry settings and a collector whose counters grow (telemetry)
// The fusion, telemetry, new-operator and grafana videos also need FUSION's stores running with some history in them.
// IKHNOS_VIDEO_OUT writes the files somewhere other than static/video. IKHNOS_SAMPLE_URL is the UI for the sample-landscape videos (default: IKHNOS_URL).
import {createRequire} from 'node:module';
import {execFileSync, spawn} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..');
// PLAYWRIGHT_FROM points at another place to resolve Playwright from, for a machine whose browsers were installed by another version.
const {chromium} = createRequire(process.env.PLAYWRIGHT_FROM ?? path.join(repo, 'package.json'))('playwright');
const SERVER = process.env.IKHNOS_URL ?? 'http://localhost:5173';
// The videos on the sample landscape run against a UI with no server behind it (npm run dev, or vite preview), at IKHNOS_SAMPLE_URL.
const SAMPLE = process.env.IKHNOS_SAMPLE_URL ?? SERVER;
let BASE = SERVER;
const OUT = process.env.IKHNOS_VIDEO_OUT ?? path.resolve(here, '..', 'static', 'video');
const W = 1280;
const H = 720;
// The page is drawn at 1920 / W times, so the video is 1920 x 1080 and zooming in on text stays sharp.
const SCALE = 1920 / W;
const CRF = 23;
const ADMIN_PASSWORD = process.env.IKHNOS_ADMIN_PASSWORD ?? 'Ikhnos-demo-2026!';

/**
 * What a screen recording of a browser lacks, drawn into the page: the pointer (with a ring where it clicks), the caption, a terminal for the
 * parts that happen outside the interface, the key chips, and the camera. The camera is a transform on the page body, so zooming in on what is
 * being typed re-draws it sharp rather than enlarging pixels; the caption and the pointer sit outside it and stay put.
 */
const OVERLAY = `
(() => {
  // A recording follows one tab, so "open in a new tab" opens in this one: the page the button reaches is the same.
  window.open = () => ({close() {}, set opener(_v) {}, location: {set href(u) { window.location.href = u; }}});
  const add = () => {
    if (document.getElementById('rec-cursor')) return;
    const st = document.createElement('style');
    st.textContent = 'html,body{height:100%;width:100%;margin:0;overflow:hidden}body{transform-origin:0 0}' +
      '@keyframes rec-ring{from{transform:translate(-50%,-50%) scale(.2);opacity:.9}to{transform:translate(-50%,-50%) scale(1);opacity:0}}';
    document.head.appendChild(st);
    const c = document.createElement('div');
    c.id = 'rec-cursor';
    c.style.cssText = 'position:fixed;z-index:2147483647;width:26px;height:26px;margin:-4px 0 0 -4px;pointer-events:none;left:0;top:0;filter:drop-shadow(0 2px 3px rgba(0,0,0,.35))';
    c.innerHTML = '<svg viewBox="0 0 24 24" width="26" height="26"><path d="M4 2l16 9-7 2-3 7z" fill="#111" stroke="#fff" stroke-width="1.6" stroke-linejoin="round"/></svg>';
    document.documentElement.appendChild(c);
    const k = document.createElement('div');
    k.id = 'rec-caption';
    k.style.cssText = 'position:fixed;z-index:2147483646;left:50%;bottom:30px;transform:translateX(-50%);max-width:76%;padding:12px 26px;border-radius:14px;background:rgba(20,23,48,.94);color:#fff;font:600 19px/1.4 Inter,system-ui,sans-serif;box-shadow:0 10px 30px rgba(0,0,0,.3);text-align:center;opacity:0;transition:opacity 260ms ease;pointer-events:none';
    document.documentElement.appendChild(k);
    window.addEventListener('mousemove', (e) => { c.style.transform = 'translate(' + e.clientX + 'px,' + e.clientY + 'px)'; }, true);
    window.addEventListener('mousedown', (e) => {
      const r = document.createElement('div');
      r.style.cssText = 'position:fixed;z-index:2147483646;left:' + e.clientX + 'px;top:' + e.clientY + 'px;width:54px;height:54px;border-radius:50%;border:3px solid #f68330;pointer-events:none;animation:rec-ring 520ms ease-out forwards';
      document.documentElement.appendChild(r);
      setTimeout(() => r.remove(), 600);
    }, true);
    window.__caption = (t, top) => { k.style.bottom = top ? 'auto' : '30px'; k.style.top = top ? '14px' : 'auto'; k.textContent = t || ''; k.style.opacity = t ? '1' : '0'; };
    // The camera: look at the point (x, y) of the page, magnified by z. z = 1 is the whole page.
    window.__cam = (x, y, z, ms) => {
      const w = window.innerWidth, h = window.innerHeight;
      const tx = Math.min(0, Math.max(w - w * z, w / 2 - x * z));
      const ty = Math.min(0, Math.max(h - h * z, h / 2 - y * z));
      document.body.style.transition = 'transform ' + ms + 'ms cubic-bezier(.45,0,.2,1)';
      document.body.style.transform = z === 1 ? '' : 'translate(' + tx + 'px,' + ty + 'px) scale(' + z + ')';
    };
    // A bubble that points at the thing to look at, with a ring round it and the rest of the page dimmed a little.
    window.__callout = (box, vis, text, side, spot, n) => {
      window.__clear(true);
      const root = document.body;
      if (spot) {
        const r = document.createElement('div');
        r.className = 'rec-callout';
        const pad = 7;
        r.style.cssText = 'position:fixed;z-index:2147483630;pointer-events:none;border-radius:12px;left:' + (box.x - pad) + 'px;top:' + (box.y - pad) + 'px;width:' + (box.w + pad * 2) + 'px;height:' + (box.h + pad * 2) + 'px;box-shadow:0 0 0 3px #f68330,0 0 0 9999px rgba(12,14,30,.3);opacity:0;transition:opacity 300ms ease';
        root.appendChild(r);
        requestAnimationFrame(() => { r.style.opacity = '1'; });
      }
      const b = document.createElement('div');
      b.className = 'rec-callout';
      b.id = 'rec-bubble';
      b.style.cssText = 'position:fixed;z-index:2147483631;pointer-events:none;max-width:330px;padding:13px 18px 13px 14px;border-radius:14px;background:#1b1f3b;color:#fff;font:600 17px/1.38 Inter,system-ui,sans-serif;display:flex;gap:11px;align-items:flex-start;box-shadow:0 14px 38px rgba(8,10,30,.45);opacity:0;transform:scale(.88);transition:opacity 240ms ease,transform 380ms cubic-bezier(.2,1.4,.4,1)';
      b.innerHTML = (n ? '<span style="flex:none;width:24px;height:24px;border-radius:50%;background:#f68330;color:#fff;font:700 14px/24px Inter,system-ui,sans-serif;text-align:center">' + n + '</span>' : '') + '<span></span>';
      b.lastChild.textContent = text;
      root.appendChild(b);
      const bw = b.offsetWidth, bh = b.offsetHeight, gap = 20;
      const place = {right: [box.x + box.w + gap, box.y + box.h / 2 - bh / 2], left: [box.x - gap - bw, box.y + box.h / 2 - bh / 2], bottom: [box.x + box.w / 2 - bw / 2, box.y + box.h + gap], top: [box.x + box.w / 2 - bw / 2, box.y - gap - bh]};
      let [bx, by] = place[side];
      const nx = Math.min(vis.x + vis.w - bw - 12, Math.max(vis.x + 12, bx));
      const ny = Math.min(vis.y + vis.h - bh - 12, Math.max(vis.y + 12, by));
      b.style.left = nx + 'px'; b.style.top = ny + 'px';
      b.style.transformOrigin = side === 'right' ? '0 50%' : side === 'left' ? '100% 50%' : side === 'top' ? '50% 100%' : '50% 0';
      const a = document.createElement('i');
      const ax = Math.min(bw - 22, Math.max(22, box.x + box.w / 2 - nx));
      const ay = Math.min(bh - 22, Math.max(22, box.y + box.h / 2 - ny));
      const pos = {right: 'left:-7px;top:' + (ay - 7) + 'px', left: 'right:-7px;top:' + (ay - 7) + 'px', bottom: 'top:-7px;left:' + (ax - 7) + 'px', top: 'bottom:-7px;left:' + (ax - 7) + 'px'}[side];
      a.style.cssText = 'position:absolute;width:14px;height:14px;background:#1b1f3b;transform:rotate(45deg);border-radius:3px;' + pos;
      b.appendChild(a);
      requestAnimationFrame(() => { b.style.opacity = '1'; b.style.transform = 'scale(1)'; });
    };
    window.__clear = (now) => {
      document.querySelectorAll('.rec-callout').forEach((e) => { if (now) e.remove(); else { e.style.opacity = '0'; setTimeout(() => e.remove(), 320); } });
    };
    // A title card over the whole picture: the logo, what this video is, and where it sits in the guide.
    window.__title = async (title, sub, ms) => {
      const wait = (m) => new Promise((r) => setTimeout(r, m));
      const d = document.createElement('div');
      d.id = 'rec-title';
      d.style.cssText = 'position:fixed;z-index:2147483647;inset:0;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;color:#fff;font-family:Inter,system-ui,sans-serif;background:radial-gradient(900px 520px at 78% 12%,rgba(246,131,48,.38),transparent 70%),linear-gradient(135deg,#1b1f3b,#323660);opacity:0;transition:opacity 450ms ease';
      d.innerHTML = '<img src="/brand/primary/ikhnos-dark.svg" height="54" style="transform:translateY(14px);opacity:0;transition:all 700ms cubic-bezier(.2,.9,.3,1) 150ms"><div style="font-size:46px;font-weight:700;letter-spacing:-.02em;transform:translateY(18px);opacity:0;transition:all 700ms cubic-bezier(.2,.9,.3,1) 350ms">' + title + '</div><div style="font-size:19px;color:#c3c8f0;transform:translateY(18px);opacity:0;transition:all 700ms cubic-bezier(.2,.9,.3,1) 550ms">' + sub + '</div>';
      document.documentElement.appendChild(d);
      await wait(30);
      d.style.opacity = '1';
      d.querySelectorAll('img,div').forEach((e) => { e.style.transform = 'none'; e.style.opacity = '1'; });
      await wait(ms);
      d.style.opacity = '0';
      await wait(500);
      d.remove();
    };
    // A key chip, for the keys a person presses outside the page (Ctrl V, Enter).
    window.__keys = (label) => {
      document.getElementById('rec-key')?.remove();
      const t = document.getElementById('rec-term');
      if (!label) return;
      const key = document.createElement('div');
      key.id = 'rec-key';
      key.textContent = label;
      if (t) {
        // In the terminal the chip sits in its title bar.
        key.style.cssText = 'position:absolute;right:14px;top:5px;padding:3px 11px;border-radius:7px;background:#f4f5f7;color:#12151a;font:600 13px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;box-shadow:0 2px 0 #b9bec7';
        t.appendChild(key);
        return;
      }
      // On the page it sits just above the field the keys go to, large enough to read at a glance.
      const r = (document.activeElement ?? document.body).getBoundingClientRect();
      key.style.cssText = 'position:fixed;z-index:2147483645;right:' + Math.max(12, window.innerWidth - r.right) + 'px;top:' + Math.max(8, r.top - 44) + 'px;padding:5px 14px;border-radius:9px;background:#f4f5f7;color:#12151a;font:600 18px/1.4 ui-monospace,SFMono-Regular,Menlo,monospace;box-shadow:0 3px 0 #b9bec7,0 8px 20px rgba(0,0,0,.25);border:1px solid #d5d9df';
      document.documentElement.appendChild(key);
    };
    const wait = (ms) => new Promise((r) => setTimeout(r, ms));
    const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;');
    const prompt = '<span style="color:#7ee787">$ </span>';
    const term = () => {
      let t = document.getElementById('rec-term');
      if (!t) {
        t = document.createElement('div');
        t.id = 'rec-term';
        t.style.cssText = 'position:fixed;z-index:2147483640;left:50%;top:50%;transform:translate(-50%,-54%);width:860px;border-radius:12px;overflow:hidden;background:#12151a;box-shadow:0 24px 70px rgba(0,0,0,.45);font:14.5px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace;color:#d8dee9';
        t.innerHTML = '<div style="display:flex;gap:7px;padding:11px 14px;background:#1b1f26"><i style="width:11px;height:11px;border-radius:50%;background:#ff5f57"></i><i style="width:11px;height:11px;border-radius:50%;background:#febc2e"></i><i style="width:11px;height:11px;border-radius:50%;background:#28c840"></i></div><pre id="rec-term-body" style="margin:0;padding:16px 18px 20px;white-space:pre-wrap;overflow-wrap:anywhere;min-height:90px"></pre>';
        const dim = document.createElement('div');
        dim.id = 'rec-dim';
        dim.style.cssText = 'position:fixed;z-index:2147483639;inset:0;background:rgba(10,12,16,.38)';
        document.body.appendChild(dim);
        document.body.appendChild(t);
      }
      return document.getElementById('rec-term-body');
    };
    // The terminal, in three beats the script can put a camera move between: the command appears (typed, or pasted all at once),
    // it is run, its output appears.
    window.__termCmd = async (cmd, paste) => {
      const body = term();
      body.textContent = '';
      if (paste) { body.innerHTML = prompt + esc(cmd); return; }
      let typed = '';
      for (const ch of cmd) { typed += ch; body.innerHTML = prompt + esc(typed); await wait(Math.min(26, 3200 / cmd.length)); }
    };
    window.__termOut = async (out) => {
      const body = term();
      body.style.fontSize = out.length > 14 ? '12.5px' : '';
      let shown = body.innerHTML;
      for (const line of out) { shown += '\\n' + esc(line); body.innerHTML = shown; await wait(200); }
    };
    // Select a piece of what the terminal printed, the way a person does before copying it.
    window.__termMark = (text) => {
      const body = term();
      body.innerHTML = body.innerHTML.split(esc(text)).join('<span style="background:#2f6feb;color:#fff;border-radius:3px;padding:0 2px">' + esc(text) + '</span>');
    };
    window.__termOff = () => { window.__clear(true); document.getElementById('rec-term')?.remove(); document.getElementById('rec-dim')?.remove(); };
  };
  if (document.readyState !== 'loading') add(); else document.addEventListener('DOMContentLoaded', add);
})();`;

/** What `helm install` prints for a chart of this repo: the install header, then the chart's own NOTES.txt (helm template does not print notes, so they are rendered through a ConfigMap). */
function helmInstallOutput(chart, release, namespace, sets, upgrade = false) {
  // A chart name is one of the server-rendered charts under backend/internal/chart; a path is a chart elsewhere in the repo.
  const src = path.isAbsolute(chart) ? chart : path.join(repo, 'backend', 'internal', 'chart', chart);
  chart = path.basename(chart);
  const dir = path.join(fs.mkdtempSync(path.join(os.tmpdir(), 'ikh-notes-')), chart);
  fs.cpSync(src, dir, {recursive: true});
  fs.writeFileSync(path.join(dir, 'templates', '_notes.tpl'), `{{- define "t.notes" -}}${fs.readFileSync(path.join(dir, 'templates', 'NOTES.txt'), 'utf8')}{{- end -}}`);
  fs.writeFileSync(path.join(dir, 'templates', 'notes.yaml'), 'apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: notes\ndata:\n  notes: {{ include "t.notes" . | quote }}\n');
  const args = ['template', release, dir, '--namespace', namespace, '--show-only', 'templates/notes.yaml', ...sets.flat()];
  const yaml = execFileSync('helm', args, {encoding: 'utf8'});
  const notes = JSON.parse(yaml.match(/^ {2}notes: (".*")$/m)[1]);
  fs.rmSync(path.dirname(dir), {recursive: true, force: true});
  const when = new Date().toString().replace(/ GMT.*/, '');
  return [upgrade ? `Release "${release}" has been upgraded. Happy Helming!` : '', 'NAME: ' + release, 'LAST DEPLOYED: ' + when, 'NAMESPACE: ' + namespace, 'STATUS: deployed', 'REVISION: ' + (upgrade ? 2 : 1), 'NOTES:', ...notes.trimEnd().split('\n')].filter((l, i) => i > 0 || l);
}

const VIDEOS = {
  /** A first look at the interface, on the sample landscape (Berlin, Athens and Paris). */
  tour: {
    title: 'A tour of the interface',
    landscape: true,
    async run(s) {
      await s.intro('/topology', 'A tour of Ikhnos', 'Every page, in a minute', {dismiss: true});
      await s.callout('nav >> text=Topology', 'Topology: your clusters and what runs in them, as one picture', {side: 'right', n: 1, ms: 3800});
      await s.click('button:has-text("Infrastructure")');
      await s.callout('button:has-text("Infrastructure")', 'Infrastructure: the same landscape as machines, capacity and links', {side: 'bottom', n: 2, ms: 3800});
      await s.click('button:has-text("Map")');
      await s.callout('button:has-text("Map")', 'Map: where the sites really are', {side: 'bottom', n: 3, ms: 3600});
      await s.click('nav >> text=Clusters');
      await s.callout('nav >> text=Clusters', 'Every kind of thing also has a table, with the same data and filters', {side: 'right', n: 4, ms: 3800});
      await s.click('button:has-text("Columns")');
      await s.callout('[role=dialog][aria-label="Choose columns"]', 'Choose the columns you want; Reset puts them back', {side: 'left', ms: 3200, spot: false});
      await s.key('Escape');
      await s.click('nav >> text=Services');
      await s.callout('nav >> text=Services', 'Services, with the cluster and namespace each one runs in', {side: 'right', n: 5, ms: 3600});
      await s.click('nav >> text=Agents');
      await s.callout('nav >> text=Agents', 'Agents: one per connected cluster, and what each may see', {side: 'right', n: 6, ms: 3800});
      await s.click('nav >> text=History');
      await s.callout('nav >> text=History', 'History: what changed, when, and what it looked like', {side: 'right', n: 7, ms: 3800});
      await s.click('[data-testid=search-button]');
      await s.callout('[role=dialog] input', 'Search finds anything: a page, a cluster, a service', {side: 'bottom', n: 8, ms: 3400});
      await s.key('Escape');
    },
  },

  /** Topology on its own: the picture, an inspector, the machines, the map with its traffic. */
  topology: {
    title: 'The topology',
    landscape: true,
    async run(s) {
      await s.intro('/topology', 'Topology', 'Your whole landscape, from cloud to far edge', {dismiss: true, fit: true});
      await s.callout('text=berlin-cloud', 'Berlin: the cloud cluster, with the platform API and ML registry', {side: 'right', n: 1, ms: 3600});
      await s.callout('text=athens-regional', 'Athens: a regional edge cluster that gathers what the sites send', {side: 'right', n: 2, ms: 3600});
      await s.callout('text=paris-edge', 'Paris: a far-edge site, with its broker and inference service', {side: 'top', n: 3, ms: 3600});
      await s.click('text=orchestrator');
      await s.callout('aside', 'Click anything to see what it is, where it runs and what it depends on', {side: 'left', n: 4, ms: 4400, spot: false});
      await s.key('Escape');
      await s.click('button:has-text("Infrastructure")');
      await s.callout('button:has-text("Infrastructure")', 'The same landscape as machines: nodes, capacity and links', {side: 'bottom', n: 5, ms: 4400});
      await s.click('button:has-text("Map")');
      await s.page.waitForTimeout(1500);
      // The map has its own zoom (a recording shows a page transform on it only some of the time), so zoom the map itself.
      await s.page.waitForSelector('text=Scroll to zoom');
      await s.page.waitForTimeout(2500);
      await s.move('text=Connections between sites');
      await s.page.mouse.move(690, 360, {steps: 25});
      for (let tries = 0; tries < 4 && (await s.page.locator('text=/· 7\\.0/').count()) > 0; tries++) {
        for (let i = 0; i < 2; i++) { await s.page.mouse.wheel(0, -120); await s.page.waitForTimeout(350); }
        await s.page.waitForTimeout(900);
      }
      await s.callout('text=Connections between sites', 'The moving dashes are traffic between sites. Faster means busier', {side: 'right', n: 6, ms: 5600});
      await s.callout('text=Paris >> nth=0', 'Click a site to see its clusters', {side: 'left', ms: 3000});
      await s.click('button:has-text("Filter")');
      await s.callout('[role=dialog][aria-label="Filter the topology"]', 'Narrow the picture by cluster, tier or application', {side: 'left', n: 7, ms: 3800, spot: false});
      await s.key('Escape');
      await s.page.waitForTimeout(800);
    },
  },

  /** Step 3 of the journey: the first sign-in on a new server (needs IKHNOS_FIRST_PASSWORD). */
  'sign-in': {
    title: 'First sign-in',
    async run(s) {
      const first = process.env.IKHNOS_FIRST_PASSWORD;
      if (!first) throw new Error('set IKHNOS_FIRST_PASSWORD to the one-time password the server printed');
      await s.intro('/', 'First sign-in', 'Your first hour, step 3: the one-time password');
      await s.callout('[role=dialog], main, body', 'A new server has one account, admin. It prints the password once, in its log', {side: 'bottom', ms: 3600, spot: false});
      await s.clear();
      await s.page.evaluate(() => window.__termCmd('', true));
      await s.zoom('#rec-term', 1.5);
      await s.termCmd("kubectl -n ikhnos logs deployment/ikhnos-server | grep -A2 'First start'", false);
      await s.page.waitForTimeout(500);
      await s.termOut(['  First start: sign in as  admin  with the one-time password  Wq7mL2xRb9eTn4cH', '  You will be asked to choose a new password straight away.']);
      await s.page.waitForTimeout(1800);
      await s.mark('Wq7mL2xRb9eTn4cH');
      await s.page.waitForTimeout(1200);
      await s.keys('Ctrl C');
      await s.page.waitForTimeout(1600);
      await s.keys('');
      await s.endTerminal();
      await s.callout('input:not([type=password])', 'Sign in as admin, and paste the password you copied', {side: 'right', n: 1, ms: 900});
      await s.type('input:not([type=password])', 'admin');
      await s.paste('input[type=password]', first);
      await s.key('Enter');
      await s.page.waitForTimeout(700);
      await s.callout('input[type=password] >> nth=0', 'The one-time password works only to set your own', {side: 'bottom', n: 2, ms: 3400});
      await s.paste('input[type=password] >> nth=0', first);
      await s.type('input[type=password] >> nth=1', ADMIN_PASSWORD);
      await s.type('input[type=password] >> nth=2', ADMIN_PASSWORD);
      await s.callout('button:has-text("Save new password")', 'The checklist ticks as the new password meets each rule', {side: 'top', n: 3, ms: 3200});
      await s.click('button:has-text("Save new password")');
      await s.page.waitForTimeout(1500);
      await s.callout('[role=dialog] input, [role=dialog]', 'Name your organisation: people you invite will see it', {side: 'bottom', n: 4, ms: 1600});
      await s.page.keyboard.type('NETMODE Lab', {delay: 90});
      await s.page.waitForTimeout(500);
      await s.click('[role=dialog] button:has-text("Save")');
      await s.callout('main', 'You are in. Nothing is connected yet, so the next step is a cluster', {side: 'bottom', ms: 4200, spot: false});
    },
  },

  /** Step 4: the Connect a cluster wizard, up to the moment the agent connects (needs IKHNOS_AGENT_CMD). */
  'connect-a-cluster': {
    title: 'Connect a cluster',
    auth: true,
    async run(s) {
      await s.intro('/topology', 'Connect a cluster', 'Your first hour, step 4: one command, one cluster');
      await s.callout('main button:has-text("Connect a cluster")', 'A new installation is empty. Start here', {side: 'left', ms: 2600});
      await s.click('main button:has-text("Connect a cluster")');
      await s.callout('[role=dialog] input', 'Give the cluster a name', {side: 'bottom', n: 1, ms: 900});
      await s.zoom(['[role=dialog] input', '#rec-bubble'], 2.1);
      await s.type('[role=dialog] input', 'athens-edge');
      await s.clear();
      await s.zoom();
      await s.callout('[data-testid=wizard-access]', 'Access: choose what the agent may read. It is always read-only, and never reads Secrets', {side: 'top', n: 2, ms: 3800});
      await s.scroll('[role=dialog] input', 380);
      await s.callout('[data-testid=wizard-collect] >> text=Collect more', 'Collect more: traffic, hardware and round-trip times stay off until you switch them on', {side: 'right', n: 3, ms: 3400});
      // Every option under "Collect more", switched on one by one.
      for (const [id, text] of [
        ['node-probe-toggle', 'Node hardware and network: a small pod on each node reads what Kubernetes cannot say'],
        ['flow-observer-toggle', 'Traffic between services: connections and bytes, never contents'],
        ['measurements-toggle', 'Round-trip times: how long a connection takes, for placement advice'],
      ]) {
        await s.callout(`[data-testid=${id}]`, text, {side: 'left', n: 3, ms: 2600});
        await s.click(`[data-testid=${id}]`);
        await s.page.waitForTimeout(700);
      }
      await s.click('button:has-text("Create install command")');
      await s.page.waitForSelector('[data-testid=install-command]');
      // The helm command is read only for the pin and token the sandbox agent needs; the video shows the controller's one command.
      const helm = await s.text('[role=dialog] pre:has-text("enrollment.key")');
      const key = helm.match(/enrollment\.key=(\S+)/)[1];
      const i = key.indexOf('.ikh_');
      await s.callout('[data-testid=install-tab-controller]', 'Or install the optional controller: one command, and it asks for the agent itself', {side: 'bottom', n: 4, ms: 3400});
      await s.click('[data-testid=install-tab-controller]');
      const cmd = await s.text('[data-testid=controller-install]');
      await s.callout('[data-testid=controller-install]', 'This server, its fingerprint and a one-time token are already in it', {side: 'bottom', n: 5, ms: 700});
      await s.zoom(['[data-testid=controller-install]', 'button:has-text("Copy command")', '#rec-bubble'], 1.5);
      await s.page.waitForTimeout(2800);
      await s.callout('button:has-text("Copy command")', 'Copy it with one click', {side: 'left', n: 6, ms: 1500});
      await s.click('button:has-text("Copy command")');
      await s.page.waitForTimeout(1300);
      await s.zoom();
      await s.page.evaluate(() => window.__termCmd('', true));
      await s.callout('#rec-term', 'Paste it in a terminal, on a machine that can reach that cluster', {side: 'top', n: 7, ms: 700});
      await s.zoom(['#rec-term', '#rec-bubble'], 1.55);
      await s.keys('Ctrl V');
      await s.page.waitForTimeout(800);
      const shown = cmd.replace(/(bootstrap\.token=)\S+/, '$1ikh_example').split('\n');
      await s.termCmd(shown.slice(0, 5).join('\n') + '\n  ...', true);
      await s.page.waitForTimeout(2200);
      await s.keys('Enter');
      await s.page.waitForTimeout(900);
      await s.keys('');
      await s.clear();
      await s.zoom('#rec-term', 1.25);
      const sets = [...cmd.matchAll(/--set(-string)? (\S+)/g)].map((m) => ['--set', m[2]]);
      await s.termOut([
        'customresourcedefinition.apiextensions.k8s.io/ikhnosagents.ikhnos.io serverside-applied',
        ...helmInstallOutput(path.join(repo, 'deploy', 'helm', 'ikhnos-controller'), 'ikhnos-controller', 'ikhnos-system', sets),
      ]);
      await s.page.waitForTimeout(3600);
      await s.endTerminal();
      s.startAgent(key.slice(0, i), key.slice(i + 1));
      await s.reveal('[role=dialog] >> text=Waiting for the agent');
      await s.callout('[role=dialog] >> text=Waiting for the agent', 'The agent dials out to your server, so the cluster needs no inbound port', {side: 'top', n: 7, ms: 4200});
      await s.clear();
      await s.callout('[role=dialog] >> text=You approve it', 'When it connects, the wizard moves on by itself', {side: 'bottom', ms: 4200});
    },
  },

  /** Step 5: reading the approval code from the agent's log and approving it. */
  approve: {
    title: 'Approve the agent',
    auth: true,
    async run(s) {
      const log = process.env.IKHNOS_AGENT_LOG;
      const code = [...fs.readFileSync(log, 'utf8').matchAll(/code=([A-Z0-9]{4}-[A-Z0-9]{4})/g)].pop()?.[1];
      if (!code) throw new Error('no approval code in ' + log);
      await s.intro('/agents', 'Approve the agent', 'Your first hour, step 5: nothing is trusted until you say so');
      await s.callout('text=athens-edge', 'The agent is waiting for you. Nothing it says is trusted until you approve it', {side: 'bottom', n: 1, ms: 3800});
      await s.clear();
      await s.page.evaluate(() => window.__termCmd('', true));
      await s.zoom('#rec-term', 1.5);
      await s.termCmd("kubectl -n ikhnos-system logs deploy/ikhnos-agent | grep -i 'approval code' | grep -oE '[A-Z0-9]{4}-[A-Z0-9]{4}'", false);
      await s.page.waitForTimeout(400);
      await s.termOut([code]);
      await s.page.waitForTimeout(2600);
      await s.endTerminal();
      await s.callout('input[placeholder="K7QM-4TXD"]', 'Type the code the agent printed in its own log. That proves you can read that cluster', {side: 'bottom', n: 2, ms: 900});
      await s.zoom(['input[placeholder="K7QM-4TXD"]', '#rec-bubble'], 1.9);
      await s.type('input[placeholder="K7QM-4TXD"]', code);
      await s.clear();
      await s.zoom();
      await s.click('button:text-is("Approve")');
      await s.callout('nav >> text=Agents', 'Approved: the agent now reports what the cluster is made of', {side: 'right', n: 3, ms: 3800, spot: false});
      await s.click('nav >> text=Clusters');
      await s.callout('text=athens-edge', 'The cluster is in the list', {side: 'bottom', n: 4, ms: 3000});
      await s.click('nav >> text=Services');
      await s.callout('main', 'and so is everything running in it, read straight from the cluster', {side: 'bottom', ms: 3400, spot: false});
      await s.click('nav >> text=Topology');
      await s.page.waitForTimeout(2500);
      await s.callout('main', 'Topology draws it: your first real cluster', {side: 'bottom', n: 5, ms: 4200, spot: false});
    },
  },
  /** FUSION: turning it on, and what its pages show (needs the FUSION stores running and a connected cluster). */
  fusion: {
    title: 'FUSION',
    auth: true,
    async run(s) {
      await s.intro('/fusion', 'FUSION', 'Where telemetry is kept: metrics, logs and traces');
      await s.callout('main h1, main', 'Metrics, logs and traces are kept here, with Grafana to look at them', {side: 'bottom', ms: 3800, spot: false});
      await s.callout('button:has-text("Turn on FUSION")', 'Turn it on. Nothing runs or is saved while it is off', {side: 'left', n: 1, ms: 3000});
      await s.click('button:has-text("Turn on FUSION")');
      await s.callout('main', 'It starts the stores one by one. Here they come up', {side: 'bottom', n: 2, ms: 1500, spot: false});
      await s.page.waitForSelector('text=Nothing needs your attention', {timeout: 90000});
      await s.callout('text=Nothing needs your attention', 'When every part is healthy, it says so', {side: 'bottom', n: 3, ms: 3200});
      await s.zoom('main', 1.5);
      await s.page.waitForTimeout(2800);
      await s.zoom();
      await s.callout('main', 'Data comes in through one door, the central operator, and is kept in three stores', {side: 'bottom', ms: 4600, spot: false});
      await s.click('main a:text-is("Data")');
      await s.callout('main a:text-is("Data")', 'Data: what has arrived, by application and service', {side: 'bottom', n: 4, ms: 4200});
      await s.click('main a:text-is("Access")');
      await s.callout('main a:text-is("Access")', 'Access: how other tools read it, with tokens you create and can revoke', {side: 'bottom', n: 5, ms: 4200});
      await s.click('main a:text-is("Settings")');
      await s.callout('main a:text-is("Settings")', 'Settings: how long each kind of data is kept', {side: 'bottom', n: 6, ms: 4200});
      await s.click('main a:text-is("Overview")');
      await s.callout('button:has-text("Open Grafana")', 'Open Grafana is one click, with no address to look up', {side: 'left', n: 7, ms: 3800});
    },
  },

  /** Setting up telemetry for a connected cluster (needs FUSION on). */
  telemetry: {
    title: 'Set up telemetry',
    auth: true,
    async run(s) {
      await s.intro('/pipeline', 'Set up telemetry', 'Choose what a cluster reports, then run two commands');
      await s.callout('main', 'Pipeline: from your clusters to FUSION, and whether each step works', {side: 'bottom', ms: 3800, spot: false});
      await s.click('main button:has-text("Set up telemetry")');
      await s.click('[role=dialog] button:has-text("Continue")');
      await s.callout('[role=dialog] button:has-text("Full infrastructure"):not(:has-text("tracing"))', 'Pick a starting point. It turns on a sensible set of signals', {side: 'right', n: 1, ms: 3200});
      await s.click('[role=dialog] button:has-text("Full infrastructure"):not(:has-text("tracing"))');
      await s.scroll('[role=dialog] button:has-text("Debug everything")', 420);
      await s.callout('[role=dialog] button:has-text("Debug everything")', 'Each signal says what it is. Tracing and the heavier ones stay off until you ask', {side: 'left', n: 2, ms: 4200});
      await s.click('[role=dialog] button:has-text("Continue")');
      await s.callout('[role=dialog]', 'Then where to send it. FUSION, on this server, is the default', {side: 'left', n: 3, ms: 4200, spot: false});
      await s.click('[role=dialog] button:has-text("Continue")');
      await s.callout('[role=dialog]', 'The server issues this cluster its own certificate, so only it may send', {side: 'left', n: 4, ms: 4200, spot: false});
      await s.click('[role=dialog] button:has-text("Generate commands")');
      await s.callout('[role=dialog]', 'The commands are shown once. Copy them and run them in that cluster', {side: 'left', n: 5, ms: 3600, spot: false});
      const text = await s.page.locator('[role=dialog]').innerText();
      const helm = text.replace(/-----BEGIN[\s\S]*?-----END [A-Z ]+-----/g, '').match(/helm upgrade [\s\S]*?(?=\nBack)/)?.[0] ?? '';
      const sets = [...helm.matchAll(/(--set(?:-string|-json)?) (\S+)/g)].map((m) => [m[1], m[2].replace(/'/g, '')]);
      const head = helm.split('\n').slice(0, 5).join('\n') + '\n  ...';
      // What the commands turn on is what the cluster's agent reports once they have been run.
      await s.clear();
      await s.page.evaluate(() => window.__termCmd('', true));
      await s.zoom('#rec-term', 1.5);
      await s.keys('Ctrl V');
      await s.page.waitForTimeout(600);
      await s.termCmd(head, true);
      await s.page.waitForTimeout(2200);
      await s.keys('Enter');
      await s.page.waitForTimeout(700);
      await s.keys('');
      await s.zoom('#rec-term', 1.25);
      const printed = helmInstallOutput('ikhnos-agent', 'ikhnos-agent', 'ikhnos-system', [['--set', 'server.address=10.0.0.5:30443'], ['--set', 'enrollment.key=sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=.ikh_example'], ...sets], true);
      // What the chart says it turned on is what the cluster's agent reports once the commands have been run.
      s.applyTelemetry((printed.join('\n').match(/Sends\s+(\S+)/)?.[1] ?? '').split(','));
      await s.termOut(printed);
      await s.page.waitForTimeout(3000);
      await s.endTerminal();
      await s.click('[role=dialog] button:has-text("I have run it"), [role=dialog] button:has-text("Check that data arrives")');
      await s.callout('[role=dialog]', 'Ikhnos then checks that the data really arrives', {side: 'left', ms: 3800, spot: false});
      await s.page.waitForSelector('[data-testid=telemetry-guided-done], [role=dialog] button:has-text("Done")', {timeout: 150000});
      await s.click('[role=dialog] button:has-text("Done")');
      // The collector the commands installed now shows on Pipeline, with the picture of what it collects and where it goes.
      await s.page.waitForSelector('text=collector', {timeout: 60000});
      await s.callout('nav >> text=Pipeline', 'The cluster now has a local operator: the collectors that were just installed', {side: 'right', n: 6, ms: 3600, spot: false});
      await s.click('main tr >> text=athens-edge collector');
      await s.page.waitForSelector('[data-testid=local-operator-detail]');
      await s.page.waitForSelector('text=Data is arriving', {timeout: 150000});
      await s.callout('[data-testid=operator-picture-body], [data-testid=local-operator-detail]', 'What it reads, the collectors, and where each kind of data goes, with its state', {side: 'top', n: 7, ms: 5200});
    },
  },

  /** Grafana through the server, over sample data (needs FUSION on and the sample telemetry feeding it). */
  grafana: {
    title: 'Grafana in FUSION',
    auth: true,
    async run(s) {
      // Grafana draws each panel after its query returns: wait for them, so the recording never shows a blank panel.
      const settle = async () => {
        await s.page.waitForLoadState('networkidle');
        await s.page.waitForFunction(() => !document.body.innerText.includes('Loading plugin panel'), null, {timeout: 30000});
        await s.page.waitForTimeout(2000);
      };
      const tag = 'Sample data, made up for this video. Your dashboards show what your clusters send';
      await s.intro('/fusion', 'Grafana in FUSION', 'Dashboards for your clusters, with no address and no login');
      await s.callout('button:has-text("Open Grafana")', 'One click: the server signs you in', {side: 'left', n: 1, ms: 2600});
      await s.click('main button:has-text("Open Grafana")');
      await s.page.waitForSelector('text=Clusters and nodes', {timeout: 60000});
      await settle();
      await s.say(tag, 4200, true);
      await s.move('main, body');
      await s.say('Clusters and nodes: readiness, CPU, memory, disk and network', 3600);
      await s.smoothScroll(520, 1800);
      await s.say('Pods and namespaces, and the deployments below their replicas', 3200);
      await s.smoothScroll(620, 1800);
      await s.say('Disk, network and the nodes that are not ready', 3200);
      await s.smoothScroll(700, 1800);
      await s.page.waitForTimeout(1500);
      for (const [uid, line] of [['ikhnos-workloads', 'Namespaces and workloads: one workload at a time, with its logs'], ['ikhnos-delivery', 'Delivery health: how long ago each cluster last sent, and through which operator']]) {
        await s.page.goto(`${BASE}/fusion/grafana/d/${uid}?orgId=1&from=now-2h&to=now&refresh=30s`);
        await settle();
        await s.say(line, 3800);
        await s.smoothScroll(480, 1800);
        await s.say(tag, 2600, true);
        await s.smoothScroll(560, 1800);
        await s.smoothScroll(560, 1800);
        await s.page.waitForTimeout(1200);
      }
    },
  },
  /** A regional operator (needs FUSION on, so it has somewhere to send). */
  'new-operator': {
    title: 'Create an operator',
    auth: true,
    async run(s) {
      await s.intro('/pipeline', 'Create an operator', 'Gather what several clusters export, and pass it on');
      await s.callout('main button:has-text("New operator")', 'An operator gathers what several clusters export and passes it on to one place', {side: 'left', n: 1, ms: 3600});
      await s.click('main button:has-text("New operator")');
      await s.callout('[role=dialog] input', 'Give it a name', {side: 'bottom', n: 2, ms: 900});
      await s.zoom(['[role=dialog] input', '#rec-bubble'], 2.0);
      await s.type('[role=dialog] input', 'edge-gateway');
      await s.clear();
      await s.zoom();
      await s.callout('[role=dialog] input', 'Where it sends. FUSION is listed because it is running on this server', {side: 'bottom', n: 3, ms: 3800, spot: false});
      await s.scroll('[role=dialog] input', 300);
      await s.callout('[role=dialog]', 'And how the other clusters reach it: a node port works on any cluster', {side: 'left', n: 4, ms: 4000, spot: false});
      await s.click('button:has-text("Create operator")');
      await s.callout('[role=dialog]', 'The commands are generated for you, and the certificates are shown once', {side: 'left', n: 5, ms: 3600, spot: false});
      const text = await s.page.locator('[role=dialog]').innerText();
      const helm = text.replace(/-----BEGIN[\s\S]*?-----END [A-Z ]+-----/g, '').match(/helm install [\s\S]*?--set service\.type=\w+/)?.[0];
      if (!helm) throw new Error('no helm install command in the dialog');
      const release = helm.match(/helm install (\S+)/)[1];
      const sets = [...helm.matchAll(/(--set(?:-string|-json)?) (\S+)/g)].map((m) => [m[1], m[2].replace(/'/g, '')]);
      await s.clear();
      await s.page.evaluate(() => window.__termCmd('', true));
      await s.zoom('#rec-term', 1.5);
      await s.keys('Ctrl V');
      await s.page.waitForTimeout(600);
      await s.termCmd(helm.split('\n').slice(0, 4).join('\n') + '\n  ...', true);
      await s.page.waitForTimeout(2200);
      await s.keys('Enter');
      await s.page.waitForTimeout(700);
      await s.keys('');
      await s.zoom('#rec-term', 1.25);
      await s.termOut(helmInstallOutput('ikhnos-regional-operator', release, 'ikhnos-system', sets));
      await s.page.waitForTimeout(3000);
      await s.endTerminal();
      await s.click('[role=dialog] button:has-text("Done")');
      await s.page.waitForTimeout(1500);
      await s.callout('text=edge-gateway', 'The operator appears on Pipeline, and turns healthy when it first reports', {side: 'bottom', n: 6, ms: 4800});
    },
  },
};

async function record(name) {
  const v = VIDEOS[name];
  BASE = v.landscape ? SAMPLE : SERVER;
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'ikh-vid-'));
  const browser = await chromium.launch();
  let storageState;
  if (v.auth) {
    // Sign in first, off camera, so the video starts inside the interface.
    const c = await browser.newContext({viewport: {width: W, height: H}, locale: 'en-US'});
    const p = await c.newPage();
    await p.goto(BASE + '/');
    await p.waitForTimeout(1200);
    await p.locator('input:not([type=password])').first().fill('admin');
    await p.locator('input[type=password]').first().fill(ADMIN_PASSWORD);
    await p.keyboard.press('Enter');
    await p.waitForTimeout(2500);
    storageState = await c.storageState();
    await c.close();
  }
  const ctx = await browser.newContext({viewport: {width: W, height: H}, deviceScaleFactor: SCALE, colorScheme: 'light', locale: 'en-US', storageState});
  await ctx.addInitScript(OVERLAY);
  if (v.landscape) {
    // Start with the European sample landscape in the browser's own storage, as if it had been the built-in one.
    const model = JSON.parse(fs.readFileSync(path.join(here, 'landscape-eu.json'), 'utf8'));
    await ctx.addInitScript(([m]) => { if (!localStorage.getItem('ikhnos/v1')) localStorage.setItem('ikhnos/v1', JSON.stringify({state: m, version: m.schemaVersion})); }, [model]);
  }
  const page = await ctx.newPage();

  // The recording is a stream of frames, each kept as the full-size JPEG the browser drew, with the time it arrived. Playwright's own video
  // recorder is capped at a low bitrate, which smears small text; frames at 1.5x are sharp, and the encoder below decides the quality.
  const cdp = await ctx.newCDPSession(page);
  const frames = [];
  const now = () => performance.now() / 1000;
  cdp.on('Page.screencastFrame', (f) => {
    const file = path.join(tmp, `f${String(frames.length).padStart(6, '0')}.jpg`);
    frames.push({t: now(), file});
    fs.writeFileSync(file, Buffer.from(f.data, 'base64'));
    cdp.send('Page.screencastFrameAck', {sessionId: f.sessionId}).catch(() => {});
  });
  await cdp.send('Page.startScreencast', {format: 'jpeg', quality: 100, maxWidth: W * SCALE, maxHeight: H * SCALE, everyNthFrame: 1});

  let x = W / 2;
  let y = H / 2;
  let cam = {z: 1, tx: 0, ty: 0};
  let cut = 0;
  const sleep = (ms) => page.waitForTimeout(ms);
  const s = {
    async go(p) {
      await page.goto(BASE + p);
      await sleep(1800);
    },
    async say(text, ms, top = false) {
      await page.evaluate(([t, up]) => window.__caption(t, up), [text, top]);
      await sleep(ms);
    },
    /**
     * Point the camera at the union of these elements and magnify it as much as fits (at most `max`), or at the whole page with no
     * argument. It eases there over `ms`, and a later call eases on from wherever it is.
     */
    async zoom(sels, max = 2, ms = 900) {
      const list = [sels].flat().filter(Boolean);
      let z = 1;
      let cx = W / 2;
      let cy = H / 2;
      if (list.length) {
        const boxes = [];
        for (const sel of list) {
          if (typeof sel === 'object') {
            // A rectangle of the screen, for what has no element to point at (a map).
            boxes.push({x: sel.x * cam.z + cam.tx, y: sel.y * cam.z + cam.ty, width: sel.w * cam.z, height: sel.h * cam.z});
            continue;
          }
          await page.locator(sel).first().scrollIntoViewIfNeeded();
          boxes.push(await page.locator(sel).first().boundingBox());
        }
        const x0 = Math.min(...boxes.map((b) => b.x));
        const y0 = Math.min(...boxes.map((b) => b.y));
        const x1 = Math.max(...boxes.map((b) => b.x + b.width));
        const y1 = Math.max(...boxes.map((b) => b.y + b.height));
        z = Math.max(1, Math.min(max, (W * 0.86) / (x1 - x0), (H * 0.8) / (y1 - y0)));
        // The boxes are where things are on screen now; the camera works in the page's own coordinates.
        cx = ((x0 + x1) / 2 - cam.tx) / cam.z;
        cy = ((y0 + y1) / 2 - cam.ty) / cam.z;
      }
      const tx = Math.min(0, Math.max(W - W * z, W / 2 - cx * z));
      const ty = Math.min(0, Math.max(H - H * z, H / 2 - cy * z));
      cam = {z, tx, ty};
      await page.evaluate(([a, b, c, d]) => window.__cam(a, b, c, d), [cx, cy, z, ms]);
      await sleep(ms + 150);
    },
    /** A bubble beside the element that says what to look at (and a ring round the element). `side` is where the bubble sits. */
    async callout(sel, text, {side = 'right', spot = true, n = 0, ms = 3000} = {}) {
      await page.locator(sel).first().scrollIntoViewIfNeeded();
      const b = await page.locator(sel).first().boundingBox();
      // The camera works in the page's own coordinates, and so does what is drawn into the page.
      const box = {x: (b.x - cam.tx) / cam.z, y: (b.y - cam.ty) / cam.z, w: b.width / cam.z, h: b.height / cam.z};
      const vis = {x: -cam.tx / cam.z, y: -cam.ty / cam.z, w: W / cam.z, h: H / cam.z};
      await page.evaluate(([a, v, t, sd, sp, nn]) => window.__callout(a, v, t, sd, sp, nn), [box, vis, text, side, spot, n]);
      await sleep(ms);
    },
    async clear() {
      await page.evaluate(() => window.__clear(false));
      await sleep(350);
    },
    /** The opening card. The page behind it loads first, off camera, so the card fades into a finished page. */
    async intro(path, title, sub, {dismiss = false, fit = false, ms = 2400} = {}) {
      await page.goto(BASE + path);
      await sleep(1800);
      if (dismiss) {
        await page.locator('button[aria-label="Dismiss"]').first().click({timeout: 3000}).catch(() => {});
        await sleep(700);
      }
      if (fit) {
        await page.locator('.react-flow__controls-fitview').first().click({timeout: 3000}).catch(() => {});
        await sleep(900);
      }
      cut = now();
      await page.evaluate(([t, u, m]) => window.__title(t, u, m), [title, sub, ms]);
      await sleep(200);
    },
    /** Click a field and type into it, key by key, at a pace a person can read. */
    async type(sel, text, delay = 85) {
      await s.click(sel);
      await page.keyboard.press('Control+a');
      for (const [i, ch] of [...text].entries()) {
        await page.keyboard.type(ch);
        await sleep(delay + ((i * 37) % 45));
      }
      await sleep(600);
    },
    /** Show a command and its output in a terminal over the page, then take it away. */
    async terminal(cmd, out, ms = 3500) {
      await page.evaluate(() => window.__caption(''));
      await page.evaluate(([c, o]) => window.__termCmd(c, false).then(() => new Promise((r) => setTimeout(r, 450))).then(() => window.__termOut(o)), [cmd, out]);
      await sleep(ms);
    },
    /** The same in three beats, so the camera can move between them. `paste` puts the command in at once, as pasting does. */
    async termCmd(cmd, paste = false) {
      await page.evaluate(() => window.__caption(''));
      await page.evaluate(([c, p]) => window.__termCmd(c, p), [cmd, paste]);
    },
    async termOut(out) {
      await page.evaluate((o) => window.__termOut(o), out);
    },
    /** Select text in the terminal, as before copying it. */
    async mark(text) {
      await page.evaluate((t) => window.__termMark(t), text);
    },
    /** Put text into a field all at once, as pasting does. */
    async paste(sel, text) {
      await s.click(sel);
      await page.keyboard.press('Control+a');
      await s.keys('Ctrl V');
      await sleep(700);
      await page.keyboard.insertText(text);
      await sleep(1100);
      await s.keys('');
    },
    /** Bring something that is below the fold of a dialog or page into the middle of the picture, smoothly. */
    async reveal(sel) {
      await page.locator(sel).first().evaluate((e) => e.scrollIntoView({block: 'center', behavior: 'smooth'}));
      await sleep(900);
    },
    async keys(label) {
      await page.evaluate((l) => window.__keys(l), label);
    },
    async endTerminal() {
      await s.zoom();
      await page.evaluate(() => window.__termOff());
      await sleep(400);
    },
    async scroll(sel, dy) {
      await s.move(sel);
      await page.mouse.wheel(0, dy);
      await sleep(900);
    },
    /** Scroll whatever scrolls on this page (the page itself, or the app's main area, or Grafana's), smoothly, by `px`. */
    async smoothScroll(px, ms = 1600) {
      await page.evaluate(async ([dy, dur]) => {
        const ok = (e) => e.scrollHeight > e.clientHeight + 20 && ['auto', 'scroll'].includes(getComputedStyle(e).overflowY) && e.clientHeight > 300;
        const el = [...document.querySelectorAll('*')].filter(ok).sort((a, b) => b.clientHeight * b.clientWidth - a.clientHeight * a.clientWidth)[0] ?? document.scrollingElement;
        const from = el.scrollTop;
        const t0 = performance.now();
        await new Promise((done) => {
          const f = (t) => {
            const u = Math.min(1, (t - t0) / dur);
            el.scrollTop = from + dy * u * u * (3 - 2 * u);
            if (u < 1) requestAnimationFrame(f); else done();
          };
          requestAnimationFrame(f);
        });
      }, [px, ms]);
      await sleep(600);
    },
    /** Start the agent for the install command just shown, with the shell command in IKHNOS_AGENT_CMD (it gets $PIN and $TOKEN). */
    startAgent(pin, token) {
      if (!process.env.IKHNOS_AGENT_CMD) throw new Error('set IKHNOS_AGENT_CMD to a command that starts an agent');
      spawn('sh', ['-c', process.env.IKHNOS_AGENT_CMD], {env: {...process.env, PIN: pin, TOKEN: token}, stdio: 'ignore', detached: true}).unref();
    },
    /** The commands the wizard showed, applied: starts the cluster's agent again the way the chart would (IKHNOS_APPLY_TELEMETRY_CMD). */
    applyTelemetry(signals) {
      if (!process.env.IKHNOS_APPLY_TELEMETRY_CMD) throw new Error('set IKHNOS_APPLY_TELEMETRY_CMD to a command that restarts the agent with telemetry on');
      spawn('sh', ['-c', process.env.IKHNOS_APPLY_TELEMETRY_CMD], {env: {...process.env, SIGNALS: signals.join(',')}, stdio: 'ignore', detached: true}).unref();
    },
    text: (sel) => page.locator(sel).first().innerText(),
    page,
    /** The pointer glides to the middle of the element, quick at the start and slow at the end, as a hand does. */
    async move(sel) {
      await page.locator(sel).first().scrollIntoViewIfNeeded();
      const box = await page.locator(sel).first().boundingBox();
      if (!box) throw new Error('not on screen: ' + sel);
      const tx = box.x + box.width / 2;
      const ty = box.y + box.height / 2;
      const ms = Math.max(450, Math.min(1100, 300 + Math.hypot(tx - x, ty - y) * 0.9));
      const t0 = now();
      for (let u = 0; u < 1; u = (now() - t0) * 1000 / ms) {
        const e = u * u * (3 - 2 * u);
        await page.mouse.move(x + (tx - x) * e, y + (ty - y) * e);
        await sleep(12);
      }
      await page.mouse.move(tx, ty);
      x = tx;
      y = ty;
    },
    async click(sel) {
      await page.evaluate(() => { window.__caption(''); window.__clear(false); });
      await s.move(sel);
      await sleep(350);
      await page.mouse.click(x, y);
      await sleep(900);
    },
    async key(k) {
      await page.keyboard.press(k);
      await sleep(700);
    },
  };
  let end;
  try {
    await v.run(s);
    await sleep(700);
    end = now();
  } catch (e) {
    await page.screenshot({path: path.join(os.tmpdir(), `ikh-fail-${name}.png`)}).catch(() => {});
    throw e;
  } finally {
    await cdp.send('Page.stopScreencast').catch(() => {});
    await ctx.close();
    await browser.close();
  }
  const first = Math.max(0, frames.findIndex((f) => f.t >= cut) - 1);
  frames.splice(0, first);
  if (!frames.length) throw new Error('no frames were captured');
  // The frames, each held until the next arrived, become a 30 fps video of the size the page was drawn at.
  const list = frames.map((f, i) => `file '${f.file}'\nduration ${((frames[i + 1]?.t ?? end) - f.t).toFixed(4)}`);
  list.push(`file '${frames.at(-1).file}'`);
  const listFile = path.join(tmp, 'frames.txt');
  fs.writeFileSync(listFile, list.join('\n'));
  fs.mkdirSync(OUT, {recursive: true});
  const mp4 = path.join(OUT, `${name}.mp4`);
  execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-f', 'concat', '-safe', '0', '-i', listFile, '-vf', 'fps=30,scale=1920:1080:flags=lanczos,format=yuv420p', '-c:v', 'libx264', '-preset', 'slow', '-crf', String(CRF), '-pix_fmt', 'yuv420p', '-movflags', '+faststart', '-an', mp4]);
  fs.writeFileSync(path.join(OUT, `${name}.jpg`), execFileSync('ffmpeg', ['-loglevel', 'error', '-ss', '3', '-i', mp4, '-frames:v', '1', '-vf', 'scale=1280:720', '-f', 'image2pipe', '-vcodec', 'mjpeg', '-q:v', '3', '-']));
  fs.rmSync(tmp, {recursive: true, force: true});
  console.log(`wrote ${name}.mp4 (${(fs.statSync(mp4).size / 1024).toFixed(0)} KB, ${frames.length} frames, ${(end - frames[0].t).toFixed(0)} s) - ${v.title}`);
}

const wanted = process.argv.slice(2);
for (const name of wanted.length ? wanted : ['tour']) await record(name);
