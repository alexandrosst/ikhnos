// Takes the screenshots the documentation and the README show, in a light and a dark version each.
//
//   node scripts/take-screenshots.mjs sample   # the sample landscape (Berlin, Athens, Paris): readme/ui-*
//   node scripts/take-screenshots.mjs server   # the first-run screens of a real, empty server: journey/*
//
// `sample` needs a UI with no server behind it, at IKHNOS_SAMPLE_URL (npm run dev, or vite preview). `server` needs an empty server at
// IKHNOS_URL with IKHNOS_FIRST_PASSWORD set to the one-time password it printed (see record-videos.mjs for the other variables).
import {createRequire} from 'node:module';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const repo = path.resolve(here, '..', '..');
const {chromium} = createRequire(process.env.PLAYWRIGHT_FROM ?? path.join(repo, 'package.json'))('playwright');
const SERVER = process.env.IKHNOS_URL ?? 'http://localhost:5173';
const SAMPLE = process.env.IKHNOS_SAMPLE_URL ?? SERVER;
const OUT = process.env.IKHNOS_IMG_OUT ?? path.resolve(here, '..', 'static', 'img');
const ADMIN_PASSWORD = process.env.IKHNOS_ADMIN_PASSWORD ?? 'Ikhnos-demo-2026!';

/** Take the same screen in both colour schemes: the interface follows the system setting, so switching it is enough. */
async function both(page, name, opts = {}) {
  for (const scheme of ['light', 'dark']) {
    await page.emulateMedia({colorScheme: scheme});
    await page.waitForTimeout(500);
    const file = path.join(OUT, `${name}-${scheme}.png`);
    fs.mkdirSync(path.dirname(file), {recursive: true});
    if (opts.element) await opts.element.screenshot({path: file});
    else await page.screenshot({path: file});
  }
  await page.emulateMedia({colorScheme: 'light'});
  console.log('wrote', name);
}

async function sample(browser) {
  const model = JSON.parse(fs.readFileSync(path.join(here, 'landscape-eu.json'), 'utf8'));
  const ctx = await browser.newContext({viewport: {width: 1280, height: 765}, deviceScaleFactor: 1.25, locale: 'en-US'});
  await ctx.addInitScript(([m]) => { if (!localStorage.getItem('ikhnos/v1')) localStorage.setItem('ikhnos/v1', JSON.stringify({state: m, version: m.schemaVersion})); }, [model]);
  const page = await ctx.newPage();
  await page.goto(SAMPLE + '/topology');
  await page.waitForTimeout(2500);
  await page.locator('button[aria-label="Dismiss"]').first().click({timeout: 3000}).catch(() => {});
  await page.locator('.react-flow__controls-fitview').first().click({timeout: 3000}).catch(() => {});
  await page.waitForTimeout(1200);
  await both(page, 'readme/ui-topology');
  await page.locator('button:has-text("Infrastructure")').first().click();
  await page.waitForTimeout(1800);
  await both(page, 'readme/ui-infrastructure');
  await page.locator('button:has-text("Map")').first().click();
  await page.waitForSelector('text=Scroll to zoom');
  await page.waitForTimeout(2500);
  await page.mouse.move(690, 360);
  for (let i = 0; i < 2; i++) { await page.mouse.wheel(0, -120); await page.waitForTimeout(350); }
  await page.waitForTimeout(1200);
  await both(page, 'readme/ui-map');
  await page.setViewportSize({width: 1190, height: 800});
  await page.goto(SAMPLE + '/placement');
  await page.waitForTimeout(2500);
  const card = page.locator('article').filter({hasText: 'Not enough evidence to recommend'}).first();
  await both(page, 'readme/ui-placement', {element: card});
  await ctx.close();
}

async function server(browser) {
  const first = process.env.IKHNOS_FIRST_PASSWORD;
  if (!first) throw new Error('set IKHNOS_FIRST_PASSWORD to the one-time password the server printed');
  const ctx = await browser.newContext({viewport: {width: 1440, height: 860}, deviceScaleFactor: 1.5, locale: 'en-US'});
  const page = await ctx.newPage();
  await page.goto(SERVER + '/');
  await page.waitForSelector('input[type=password]');
  await page.waitForTimeout(800);
  await both(page, 'journey/signin');
  await page.locator('input:not([type=password])').first().fill('admin');
  await page.locator('input[type=password]').first().fill(first);
  await page.keyboard.press('Enter');
  await page.waitForSelector('text=Save new password');
  await page.waitForTimeout(800);
  await both(page, 'journey/new-password');
  await page.setViewportSize({width: 1440, height: 900});
  await page.locator('input[type=password]').nth(0).fill(first);
  await page.locator('input[type=password]').nth(1).fill(ADMIN_PASSWORD);
  await page.locator('input[type=password]').nth(2).fill(ADMIN_PASSWORD);
  await page.locator('button:has-text("Save new password")').click();
  await page.waitForSelector('[role=dialog]');
  await page.keyboard.type('NETMODE Lab');
  await page.locator('[role=dialog] button:has-text("Save")').click();
  await page.waitForSelector('text=Getting started');
  await page.waitForTimeout(1500);
  await both(page, 'journey/first-run');
  await page.locator('main button:has-text("Connect a cluster")').first().click();
  await page.waitForSelector('[role=dialog] input');
  await page.locator('[role=dialog] input').first().fill('athens-edge');
  await page.waitForTimeout(500);
  await both(page, 'journey/connect-name');
  await page.locator('[role=dialog] input').first().scrollIntoViewIfNeeded();
  await page.mouse.move(720, 500);
  await page.mouse.wheel(0, 380);
  await page.waitForTimeout(700);
  await both(page, 'journey/connect-options');
  await page.locator('button:has-text("Create install command")').click();
  await page.waitForSelector('[data-testid=install-command]');
  await page.waitForTimeout(900);
  await both(page, 'journey/connect-command');
  await ctx.close();
}

const browser = await chromium.launch();
try {
  const what = process.argv[2];
  if (what === 'sample') await sample(browser);
  else if (what === 'server') await server(browser);
  else throw new Error('say which: sample or server');
} finally {
  await browser.close();
}
