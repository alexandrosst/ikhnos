// Records the short screen videos the documentation shows, from the real UI, and writes them to static/video as MP4.
//
//   node scripts/record-videos.mjs [name ...]      # from docs-site/; no name records the tour. The UI runs at IKHNOS_URL, default http://localhost:5173 (npm run dev, repo root)
//
// Each video is a script of steps: move the pointer to something, click it, say what is on screen in a caption, wait. There is no
// narration, so the captions are the explanation and the videos work with the sound off. Re-run after the UI changes.
//
// The tour runs on the built-in sample landscape. The others need a real server, so they run against IKHNOS_URL, in this order, on an
// empty one (sign-in, connect-a-cluster, approve), with these set:
//   IKHNOS_FIRST_PASSWORD   the one-time admin password the server printed at its first start (sign-in)
//   IKHNOS_ADMIN_PASSWORD   the password the sign-in video chooses, which later videos sign in with (default below)
//   IKHNOS_AGENT_CMD        a shell command that starts an agent for the cluster to connect; it gets the install command's pin and
//                           token as $PIN and $TOKEN (connect-a-cluster)
//   IKHNOS_AGENT_LOG        the file that agent writes its log to, for the approval code (approve)
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
const BASE = process.env.IKHNOS_URL ?? 'http://localhost:5173';
const OUT = path.resolve(here, '..', 'static', 'video');
const W = 1280;
const H = 720;
const ADMIN_PASSWORD = process.env.IKHNOS_ADMIN_PASSWORD ?? 'Ikhnos-demo-2026!';

/** The pointer and the caption are drawn into the page, since a screen recording of a browser has neither. */
const OVERLAY = `
(() => {
  // A recording follows one tab, so "open in a new tab" opens in this one: the page the button reaches is the same.
  window.open = () => ({close() {}, set opener(_v) {}, location: {set href(u) { window.location.href = u; }}});
  const add = () => {
    if (document.getElementById('rec-cursor')) return;
    const c = document.createElement('div');
    c.id = 'rec-cursor';
    c.style.cssText = 'position:fixed;z-index:2147483647;width:18px;height:18px;margin:-3px 0 0 -3px;pointer-events:none;transition:transform 80ms linear;left:0;top:0';
    c.innerHTML = '<svg viewBox="0 0 24 24" width="18" height="18"><path d="M4 2l16 9-7 2-3 7z" fill="#111" stroke="#fff" stroke-width="1.6" stroke-linejoin="round"/></svg>';
    document.documentElement.appendChild(c);
    const k = document.createElement('div');
    k.id = 'rec-caption';
    k.style.cssText = 'position:fixed;z-index:2147483646;left:50%;bottom:26px;transform:translateX(-50%);max-width:80%;padding:10px 18px;border-radius:10px;background:rgba(17,20,24,.88);color:#fff;font:500 17px/1.4 Inter,system-ui,sans-serif;text-align:center;opacity:0;transition:opacity 220ms ease;pointer-events:none';
    document.documentElement.appendChild(k);
    window.addEventListener('mousemove', (e) => { c.style.transform = 'translate(' + e.clientX + 'px,' + e.clientY + 'px)'; }, true);
    window.__caption = (t, top) => { k.style.bottom = top ? 'auto' : '26px'; k.style.top = top ? '10px' : 'auto'; k.textContent = t || ''; k.style.opacity = t ? '1' : '0'; };
    // A terminal for the parts that happen outside the interface: the command is typed, then its output appears.
    window.__term = async (cmd, out) => {
      const wait = (ms) => new Promise((r) => setTimeout(r, ms));
      let t = document.getElementById('rec-term');
      if (!t) {
        t = document.createElement('div');
        t.id = 'rec-term';
        t.style.cssText = 'position:fixed;z-index:2147483645;left:50%;top:50%;transform:translate(-50%,-54%);width:860px;border-radius:12px;overflow:hidden;background:#12151a;box-shadow:0 24px 70px rgba(0,0,0,.45);font:14.5px/1.55 ui-monospace,SFMono-Regular,Menlo,monospace;color:#d8dee9';
        t.innerHTML = '<div style="display:flex;gap:7px;padding:11px 14px;background:#1b1f26"><i style="width:11px;height:11px;border-radius:50%;background:#ff5f57"></i><i style="width:11px;height:11px;border-radius:50%;background:#febc2e"></i><i style="width:11px;height:11px;border-radius:50%;background:#28c840"></i></div><pre id="rec-term-body" style="margin:0;padding:16px 18px 20px;white-space:pre-wrap;overflow-wrap:anywhere;min-height:90px"></pre>';
        document.documentElement.appendChild(t);
      }
      const body = document.getElementById('rec-term-body');
      body.textContent = '';
      body.style.fontSize = out.length > 14 ? '12.5px' : '';
      const esc = (s) => s.replace(/&/g, '&amp;').replace(/</g, '&lt;');
      const prompt = '<span style="color:#7ee787">$ </span>';
      let typed = '';
      for (const ch of cmd) { typed += ch; body.innerHTML = prompt + esc(typed); await wait(Math.min(26, 3200 / cmd.length)); }
      await wait(450);
      let shown = body.innerHTML;
      for (const line of out) { shown += '\\n' + esc(line); body.innerHTML = shown; await wait(220); }
    };
    window.__termOff = () => { document.getElementById('rec-term')?.remove(); };
  };
  if (document.readyState !== 'loading') add(); else document.addEventListener('DOMContentLoaded', add);
})();`;


/** What `helm install` prints for a chart of this repo: the install header, then the chart's own NOTES.txt (helm template does not print notes, so they are rendered through a ConfigMap). */
function helmInstallOutput(chart, release, namespace, sets, upgrade = false) {
  const src = path.join(repo, 'backend', 'internal', 'chart', chart);
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
  /** A first look at the interface, on the built-in sample landscape. */
  tour: {
    title: 'A tour of the interface',
    async run(s) {
      await s.go('/topology');
      await s.say('Topology: your clusters, and what runs in each, from cloud to far edge', 4500);
      await s.say('Every box is a cluster; the lines are the dependencies between services', 4000);
      await s.click('button:has-text("Infrastructure")');
      await s.say('Infrastructure: the same landscape as nodes, capacity and links', 4500);
      await s.click('button:has-text("Map")');
      await s.say('Map: where the sites really are', 4000);
      await s.click('nav >> text=Clusters');
      await s.say('Every kind of thing also has a table, with the same data and filters', 4500);
      await s.click('button:has-text("Columns")');
      await s.say('Choose the columns you want; Reset puts them back', 3500);
      await s.key('Escape');
      await s.click('nav >> text=Services');
      await s.say('Services, with the cluster and namespace each one runs in', 4000);
      await s.click('nav >> text=Agents');
      await s.say('Agents: one per connected cluster, with what each may see', 4500);
      await s.click('nav >> text=History');
      await s.say('History: what changed, when, and what it looked like', 4500);
      await s.key('Control+k');
      await s.say('Ctrl K finds anything: a page, a cluster, a service', 3500);
      await s.key('Escape');
    },
  },
  /** Step 3 of the journey: the first sign-in on a new server (needs IKHNOS_FIRST_PASSWORD). */
  'sign-in': {
    title: 'First sign-in',
    async run(s) {
      const first = process.env.IKHNOS_FIRST_PASSWORD;
      if (!first) throw new Error('set IKHNOS_FIRST_PASSWORD to the one-time password the server printed');
      await s.go('/');
      await s.say('A new server has one account, admin, and prints its password once, in its log', 3800);
      await s.terminal(
        "kubectl -n ikhnos logs deployment/ikhnos-server | grep -A2 'First start'",
        ['  First start: sign in as  admin  with the one-time password  Wq7mL2xRb9eTn4cH', '  You will be asked to choose a new password straight away.'],
        4800,
      );
      await s.endTerminal();
      await s.say('Sign in as admin with that password', 1600);
      await s.type('input:not([type=password])', 'admin');
      await s.type('input[type=password]', first);
      await s.key('Enter');
      await s.say('The one-time password works only to set your own', 3800, true);
      await s.type('input[type=password] >> nth=0', first);
      await s.type('input[type=password] >> nth=1', ADMIN_PASSWORD);
      await s.type('input[type=password] >> nth=2', ADMIN_PASSWORD);
      await s.say('The checklist ticks as the new password meets each rule', 3200, true);
      await s.click('button:has-text("Save new password")');
      await s.page.waitForTimeout(1500);
      await s.say('Name your organisation: people you invite will see it', 3200, true);
      await s.page.keyboard.type('NETMODE Lab', {delay: 80});
      await s.page.waitForTimeout(500);
      await s.click('[role=dialog] button:has-text("Save")');
      await s.say('You are in. Nothing is connected yet, so the next step is a cluster', 4200);
    },
  },

  /** Step 4: the Connect a cluster wizard, up to the moment the agent connects (needs IKHNOS_AGENT_CMD). */
  'connect-a-cluster': {
    title: 'Connect a cluster',
    auth: true,
    async run(s) {
      await s.go('/topology');
      await s.say('A new installation is empty. Connect a cluster adds the first one', 3800);
      await s.click('main button:has-text("Connect a cluster")');
      await s.say('Give the cluster a name', 2200, true);
      await s.type('[role=dialog] input', 'athens-edge');
      await s.say('Choose what the agent may read. It is always read-only, and never reads Secrets', 4200, true);
      await s.scroll('[role=dialog] input', 380);
      await s.say('Traffic, hardware and round-trip times stay off until you switch them on', 4200, true);
      await s.click('button:has-text("Create install command")');
      await s.say('One command, filled in for you: this server, its certificate fingerprint, a one-time token', 5200, true);
      const cmd = await s.text('[role=dialog] pre:has-text("enrollment.key")');
      const key = cmd.match(/enrollment\.key=(\S+)/)[1];
      const i = key.indexOf('.ikh_');
      await s.say('Run it where you use helm for that cluster', 1800, true);
      await s.terminal(cmd.replace(/\.ikh_\S+/, '.ikh_example'), helmInstallOutput('ikhnos-agent', 'ikhnos-agent', 'ikhnos-system', [['--set', 'server.address=10.0.0.5:30443'], ['--set', 'enrollment.key=' + key]]), 6500);
      await s.endTerminal();
      s.startAgent(key.slice(0, i), key.slice(i + 1));
      await s.say('The agent dials out to the server, so the cluster needs no inbound port', 3600, true);
      await s.say('When it connects, the wizard moves on by itself', 5200, true);
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
      await s.go('/agents');
      await s.say('The agent is waiting for you. Nothing it says is trusted until you approve it', 4200);
      await s.say('It printed a code in its own log, and nowhere else', 3200);
      await s.terminal("kubectl -n ikhnos-system logs deploy/ikhnos-agent | grep -i 'approval code' | grep -oE '[A-Z0-9]{4}-[A-Z0-9]{4}'", [code], 3800);
      await s.endTerminal();
      await s.type('input[placeholder="K7QM-4TXD"]', code);
      await s.say('Typing it back proves you can read that cluster', 3200);
      await s.click('button:text-is("Approve")');
      await s.say('Approved: the agent now reports what the cluster is made of', 4200);
      await s.click('nav >> text=Clusters');
      await s.say('The cluster is in the list', 3200);
      await s.click('nav >> text=Services');
      await s.say('and so is everything running in it, read straight from the cluster', 4200);
      await s.click('nav >> text=Topology');
      await s.say('Topology draws it', 4200);
    },
  },
  /** FUSION: turning it on, and what its pages show (needs the FUSION stores running and a connected cluster). */
  fusion: {
    title: 'FUSION',
    auth: true,
    async run(s) {
      await s.go('/fusion');
      await s.say('FUSION is the place telemetry is kept: metrics, logs and traces, with Grafana to look at them', 4800);
      await s.click('button:has-text("Turn on FUSION")');
      await s.say('Turning it on starts the stores. Nothing is running or saved while it is off', 4200);
      await s.page.waitForSelector('text=Nothing needs your attention', {timeout: 60000});
      await s.say('When every part is healthy, it says so', 3200);
      await s.say('Data comes in through one door, the central operator, and is kept in three stores', 5200);
      await s.click('main a:text-is("Data")');
      await s.say('Data: what has arrived, by application and service, so you can check that it is', 4800);
      await s.click('main a:text-is("Access")');
      await s.say('Access: how other tools read it, with tokens you create and can revoke', 4800);
      await s.click('main a:text-is("Settings")');
      await s.say('Settings: how long each kind of data is kept', 4800);
      await s.click('main a:text-is("Overview")');
      await s.say('Open Grafana is one click, with no address to look up', 3600);
    },
  },

  /** Setting up telemetry for a connected cluster (needs FUSION on). */
  telemetry: {
    title: 'Set up telemetry',
    auth: true,
    async run(s) {
      await s.go('/pipeline');
      await s.say('Pipeline: from your clusters to FUSION, and whether each step works', 4000);
      await s.click('main button:has-text("Set up telemetry")');
      await s.click('[role=dialog] button:has-text("Continue")');
      await s.say('Choose what the cluster should report', 3200, true);
      await s.click('[role=dialog] button:has-text("Full infrastructure"):not(:has-text("tracing"))');
      await s.scroll('[role=dialog] button:has-text("Debug everything")', 420);
      await s.say('Each signal says what it is. A starting point turns on a sensible set', 4200, true);
      await s.click('[role=dialog] button:has-text("Continue")');
      await s.say('Then where to send it. FUSION, on this server, is the default', 4800, true);
      await s.click('[role=dialog] button:has-text("Continue")');
      await s.say('The server issues this cluster its own certificate, so only it may send', 4800, true);
      await s.click('[role=dialog] button:has-text("Generate commands")');
      await s.say('The commands are shown once. Run them in that cluster', 4200, true);
      const text = await s.page.locator('[role=dialog]').innerText();
      const helm = text.replace(/-----BEGIN[\s\S]*?-----END [A-Z ]+-----/g, '').match(/helm upgrade [\s\S]*?(?=\nBack)/)?.[0] ?? '';
      const sets = [...helm.matchAll(/(--set(?:-string|-json)?) (\S+)/g)].map((m) => [m[1], m[2].replace(/'/g, '')]);
      const head = helm.split('\n').slice(0, 5).join('\n') + '\n  ...';
      await s.terminal(head, helmInstallOutput('ikhnos-agent', 'ikhnos-agent', 'ikhnos-system', [['--set', 'server.address=10.0.0.5:30443'], ['--set', 'enrollment.key=sha256/AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=.ikh_example'], ...sets], true), 6500);
      await s.endTerminal();
      await s.say('Pipeline then shows whether data is arriving, step by step', 4200, true);
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
      await s.go('/fusion');
      await s.say('Open Grafana: the server signs you in, so there is no address and no login', 4200);
      await s.click('main button:has-text("Open Grafana")');
      await s.page.waitForSelector('text=Clusters and nodes', {timeout: 60000});
      await settle();
      await s.say(tag, 4200, true);
      const down = async (px, ms = 700) => {
        for (let i = 0; i < px / 110; i++) {
          await s.page.mouse.wheel(0, 110);
          await s.page.waitForTimeout(ms / 6);
        }
        await s.page.waitForTimeout(900);
      };
      await s.move('main, body');
      await s.say('Clusters and nodes: readiness, CPU, memory, disk and network', 3600);
      await down(900);
      await s.say('Pods and namespaces, and the deployments that are below their replicas', 4200);
      await down(1100);
      for (const [uid, line] of [['ikhnos-workloads', 'Namespaces and workloads: one workload at a time, with its logs'], ['ikhnos-delivery', 'Delivery health: how long ago each cluster last sent, and through which operator']]) {
        await s.page.goto(`${BASE}/fusion/grafana/d/${uid}?orgId=1&from=now-2h&to=now&refresh=30s`);
        await settle();
        await s.say(line, 4200);
        await down(1000);
        await s.say(tag, 3200, true);
        await down(900);
      }
    },
  },
  /** A regional operator (needs FUSION on, so it has somewhere to send). */
  'new-operator': {
    title: 'Create an operator',
    auth: true,
    async run(s) {
      await s.go('/pipeline');
      await s.say('An operator gathers what several clusters export and passes it on to one place', 4200);
      await s.click('main button:has-text("New operator")');
      await s.say('Give it a name', 2200, true);
      await s.type('[role=dialog] input', 'edge-gateway');
      await s.say('Where it sends. FUSION is listed because it is running on this server', 4800, true);
      await s.scroll('[role=dialog] input', 300);
      await s.say('And how the other clusters reach it: a node port works on any cluster', 4800, true);
      await s.click('button:has-text("Create operator")');
      await s.say('The commands are generated for you, and the certificates are shown once', 4800, true);
      const text = await s.page.locator('[role=dialog]').innerText();
      const helm = text.replace(/-----BEGIN[\s\S]*?-----END [A-Z ]+-----/g, '').match(/helm install [\s\S]*?--set service\.type=\w+/)?.[0];
      if (!helm) throw new Error('no helm install command in the dialog');
      const release = helm.match(/helm install (\S+)/)[1];
      const sets = [...helm.matchAll(/(--set(?:-string|-json)?) (\S+)/g)].map((m) => [m[1], m[2].replace(/'/g, '')]);
      await s.say('Run them in the cluster where the operator should live', 3200, true);
      await s.terminal(helm.split('\n').slice(0, 4).join('\n') + '\n  ...', helmInstallOutput('ikhnos-regional-operator', release, 'ikhnos-system', sets), 7000);
      await s.endTerminal();
      await s.click('[role=dialog] button:has-text("Done")');
      await s.say('The operator appears on Pipeline, and turns healthy when it first reports', 4800);
    },
  },
};

async function record(name) {
  const v = VIDEOS[name];
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
  const ctx = await browser.newContext({viewport: {width: W, height: H}, recordVideo: {dir: tmp, size: {width: W, height: H}}, colorScheme: 'light', locale: 'en-US', storageState});
  await ctx.addInitScript(OVERLAY);
  const page = await ctx.newPage();
  let x = 640;
  let y = 360;
  const s = {
    async go(p) {
      await page.goto(BASE + p);
      await page.waitForTimeout(1800);
    },
    async say(text, ms, top = false) {
      await page.evaluate(([t, up]) => window.__caption(t, up), [text, top]);
      await page.waitForTimeout(ms);
    },
    /** Click a field and type into it, key by key. */
    async type(sel, text) {
      await s.click(sel);
      await page.keyboard.press('Control+a');
      await page.keyboard.type(text, {delay: 70});
      await page.waitForTimeout(500);
    },
    /** Show a command and its output in a terminal over the page, then take it away. */
    async terminal(cmd, out, ms = 3500) {
      await page.evaluate(() => window.__caption(''));
      await page.evaluate(([c, o]) => window.__term(c, o), [cmd, out]);
      await page.waitForTimeout(ms);
    },
    async endTerminal() {
      await page.evaluate(() => window.__termOff());
      await page.waitForTimeout(400);
    },
    async scroll(sel, dy) {
      await s.move(sel);
      await page.mouse.wheel(0, dy);
      await page.waitForTimeout(900);
    },
    /** Start the agent for the install command just shown, with the shell command in IKHNOS_AGENT_CMD (it gets $PIN and $TOKEN). */
    startAgent(pin, token) {
      if (!process.env.IKHNOS_AGENT_CMD) throw new Error('set IKHNOS_AGENT_CMD to a command that starts an agent');
      spawn('sh', ['-c', process.env.IKHNOS_AGENT_CMD], {env: {...process.env, PIN: pin, TOKEN: token}, stdio: 'ignore', detached: true}).unref();
    },
    text: (sel) => page.locator(sel).first().innerText(),
    page,
    async move(sel) {
      await page.locator(sel).first().scrollIntoViewIfNeeded();
      const box = await page.locator(sel).first().boundingBox();
      if (!box) throw new Error('not on screen: ' + sel);
      const tx = box.x + box.width / 2;
      const ty = box.y + box.height / 2;
      await page.mouse.move(tx, ty, {steps: 28});
      x = tx;
      y = ty;
    },
    async click(sel) {
      await page.evaluate(() => window.__caption(''));
      await s.move(sel);
      await page.waitForTimeout(350);
      await page.mouse.click(x, y);
      await page.waitForTimeout(900);
    },
    async key(k) {
      await page.keyboard.press(k);
      await page.waitForTimeout(700);
    },
  };
  try {
    await v.run(s);
    await page.waitForTimeout(600);
  } catch (e) {
    await page.screenshot({path: path.join(os.tmpdir(), `ikh-fail-${name}.png`)}).catch(() => {});
    throw e;
  } finally {
    await ctx.close();
    await browser.close();
  }
  const webm = fs.readdirSync(tmp).find((f) => f.endsWith('.webm'));
  fs.mkdirSync(OUT, {recursive: true});
  const mp4 = path.join(OUT, `${name}.mp4`);
  execFileSync('ffmpeg', ['-y', '-loglevel', 'error', '-i', path.join(tmp, webm), '-c:v', 'libx264', '-preset', 'slow', '-crf', '28', '-pix_fmt', 'yuv420p', '-movflags', '+faststart', '-an', mp4]);
  fs.writeFileSync(path.join(OUT, `${name}.jpg`), execFileSync('ffmpeg', ['-loglevel', 'error', '-ss', '3', '-i', mp4, '-frames:v', '1', '-f', 'image2pipe', '-vcodec', 'mjpeg', '-q:v', '4', '-']));
  fs.rmSync(tmp, {recursive: true, force: true});
  console.log(`wrote ${name}.mp4 (${(fs.statSync(mp4).size / 1024).toFixed(0)} KB) - ${v.title}`);
}

const wanted = process.argv.slice(2);
for (const name of wanted.length ? wanted : ['tour']) await record(name);
