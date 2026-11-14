// Records the short screen videos the documentation shows, from the real UI, and writes them to static/video as MP4.
//
//   node scripts/record-videos.mjs [tour ...]      # from docs-site/, with the UI running: npm run dev (repo root), default http://localhost:5173
//
// Each video is a script of steps: move the pointer to something, click it, say what is on screen in a caption, wait. There is no
// narration, so the captions are the explanation and the videos work with the sound off. Re-run after the UI changes.
import {createRequire} from 'node:module';
import {execFileSync} from 'node:child_process';
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

/** The pointer and the caption are drawn into the page, since a screen recording of a browser has neither. */
const OVERLAY = `
(() => {
  const add = () => {
    if (document.getElementById('rec-cursor')) return;
    const c = document.createElement('div');
    c.id = 'rec-cursor';
    c.style.cssText = 'position:fixed;z-index:2147483647;width:18px;height:18px;margin:-3px 0 0 -3px;pointer-events:none;transition:transform 80ms linear;left:0;top:0';
    c.innerHTML = '<svg viewBox="0 0 24 24" width="18" height="18"><path d="M4 2l16 9-7 2-3 7z" fill="#111" stroke="#fff" stroke-width="1.6" stroke-linejoin="round"/></svg>';
    document.documentElement.appendChild(c);
    const k = document.createElement('div');
    k.id = 'rec-caption';
    k.style.cssText = 'position:fixed;z-index:2147483646;left:50%;bottom:26px;transform:translateX(-50%);max-width:70%;padding:10px 18px;border-radius:10px;background:rgba(17,20,24,.88);color:#fff;font:500 17px/1.4 Inter,system-ui,sans-serif;text-align:center;opacity:0;transition:opacity 220ms ease;pointer-events:none';
    document.documentElement.appendChild(k);
    window.addEventListener('mousemove', (e) => { c.style.transform = 'translate(' + e.clientX + 'px,' + e.clientY + 'px)'; }, true);
    window.__caption = (t) => { k.textContent = t || ''; k.style.opacity = t ? '1' : '0'; };
  };
  if (document.readyState !== 'loading') add(); else document.addEventListener('DOMContentLoaded', add);
})();`;

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
};

async function record(name) {
  const v = VIDEOS[name];
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'ikh-vid-'));
  const browser = await chromium.launch();
  const ctx = await browser.newContext({viewport: {width: W, height: H}, recordVideo: {dir: tmp, size: {width: W, height: H}}, colorScheme: 'light'});
  await ctx.addInitScript(OVERLAY);
  const page = await ctx.newPage();
  let x = 640;
  let y = 360;
  const s = {
    async go(p) {
      await page.goto(BASE + p);
      await page.waitForTimeout(1800);
    },
    async say(text, ms) {
      await page.evaluate((t) => window.__caption(t), text);
      await page.waitForTimeout(ms);
    },
    async move(sel) {
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
for (const name of wanted.length ? wanted : Object.keys(VIDEOS)) await record(name);
