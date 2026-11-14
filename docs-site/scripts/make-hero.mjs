// Makes the looping picture at the top of the README and of the documentation: the sample landscape on the map, with the traffic moving
// along the links between Berlin, Paris and Athens. It records the page, then turns the frames into an animated WebP (about 8 s, a light and a dark one).
//
//   node scripts/make-hero.mjs        # from docs-site/; writes static/img/readme/topology-loop-{light,dark}.webp
//
// It needs a UI with no server behind it, at IKHNOS_SAMPLE_URL (npm run dev, or vite preview), and ffmpeg.
import {createRequire} from 'node:module';
import {execFileSync} from 'node:child_process';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..');
const {chromium} = createRequire(process.env.PLAYWRIGHT_FROM ?? path.join(repo, 'package.json'))('playwright');
const BASE = process.env.IKHNOS_SAMPLE_URL ?? 'http://localhost:5173';
const OUT = process.env.IKHNOS_IMG_OUT ?? path.resolve(here, '..', 'static', 'img', 'readme');
const SECONDS = 8;
const FPS = 15;
const WIDTH = 1440;
const QUALITY = 92;

async function loop(browser, scheme) {
  const model = JSON.parse(fs.readFileSync(path.join(here, 'landscape-eu.json'), 'utf8'));
  const ctx = await browser.newContext({viewport: {width: 1280, height: 765}, deviceScaleFactor: 1.5, colorScheme: scheme, locale: 'en-US'});
  await ctx.addInitScript(([m]) => { if (!localStorage.getItem('ikhnos/v1')) localStorage.setItem('ikhnos/v1', JSON.stringify({state: m, version: m.schemaVersion})); }, [model]);
  const page = await ctx.newPage();
  await page.goto(BASE + '/topology');
  await page.waitForTimeout(2000);
  await page.locator('button[aria-label="Dismiss"]').first().click({timeout: 3000}).catch(() => {});
  await page.locator('button:has-text("Map")').first().click();
  await page.waitForSelector('text=Scroll to zoom');
  await page.waitForTimeout(2500);
  await page.mouse.move(690, 360);
  for (let i = 0; i < 2; i++) { await page.mouse.wheel(0, -120); await page.waitForTimeout(350); }
  await page.mouse.move(1270, 760);
  await page.waitForTimeout(1500);

  // Frames as the browser draws them, kept with the time they arrived.
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), 'ikh-hero-'));
  const cdp = await ctx.newCDPSession(page);
  const frames = [];
  cdp.on('Page.screencastFrame', (f) => {
    const file = path.join(tmp, `f${String(frames.length).padStart(5, '0')}.jpg`);
    frames.push({t: performance.now() / 1000, file});
    fs.writeFileSync(file, Buffer.from(f.data, 'base64'));
    cdp.send('Page.screencastFrameAck', {sessionId: f.sessionId}).catch(() => {});
  });
  await cdp.send('Page.startScreencast', {format: 'jpeg', quality: 100, maxWidth: 1920, maxHeight: 1148, everyNthFrame: 1});
  await page.waitForTimeout(SECONDS * 1000 + 300);
  await cdp.send('Page.stopScreencast');
  const end = performance.now() / 1000;
  await ctx.close();

  const list = frames.map((f, i) => `file '${f.file}'\nduration ${((frames[i + 1]?.t ?? end) - f.t).toFixed(4)}`);
  list.push(`file '${frames.at(-1).file}'`);
  fs.writeFileSync(path.join(tmp, 'frames.txt'), list.join('\n'));
  const scale = `fps=${FPS},scale=${WIDTH}:-1:flags=lanczos`;
  const input = ['-f', 'concat', '-safe', '0', '-t', String(SECONDS), '-i', path.join(tmp, 'frames.txt')];
  const out = path.join(OUT, `topology-loop-${scheme}.webp`);
  fs.mkdirSync(OUT, {recursive: true});
  // Animated WebP, not GIF: a GIF has 256 colours, which turns the soft map and the thin lines into noise.
  execFileSync('ffmpeg', ['-y', '-loglevel', 'error', ...input, '-vf', scale, '-c:v', 'libwebp_anim', '-quality', String(QUALITY), '-compression_level', '6', '-loop', '0', out]);
  fs.rmSync(tmp, {recursive: true, force: true});
  console.log(`wrote ${path.basename(out)} (${(fs.statSync(out).size / 1024).toFixed(0)} KB, ${frames.length} frames captured)`);
}

const browser = await chromium.launch();
try {
  for (const scheme of ['light', 'dark']) await loop(browser, scheme);
} finally {
  await browser.close();
}
