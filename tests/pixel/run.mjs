// Pixel comparison of the approved design boards and the live hub.
//   npm run test:pixel                       every mapped board, light and dark
//   npm run test:pixel -- --only=Dashboard,Repo --themes=light --concurrency=2
// Environment: PIXEL_BOARDS_DIR (the .dc.html boards), PIXEL_REPORT_DIR (the report), PIXEL_HUB_BIN (hub binary
// for the throw-away hub), PIXEL_HUB_URL (use a hub that is already running and seeded instead).
import {execFileSync, spawnSync} from 'node:child_process';
import {createServer} from 'node:http';
import {existsSync, mkdirSync, readFileSync, rmSync, writeFileSync} from 'node:fs';
import {join, resolve} from 'node:path';
import {chromium} from '@playwright/test';
import sharp from 'sharp';
import {ANCHOR, BOARDS_DIR, HUB_URL, OUT_DIR, PASSWORD, PIXEL_DIR, PORT, REPO_ROOT, STATE_FILE} from './config.mjs';
import {compareStructure, comparePixels, deltas} from './compare.mjs';
import {hideBoardStates, measure, settle} from './probe.mjs';
import {writeReport} from './report.mjs';
import {seed} from './seed.mjs';

const args = Object.fromEntries(process.argv.slice(2).map((a) => { const [k, v] = a.replace(/^--/, '').split('='); return [k, v ?? true]; }));
const only = args.only ? String(args.only).split(',') : null;
const themes = String(args.themes || process.env.PIXEL_THEMES || 'light,dark').split(',');
const concurrency = Number(args.concurrency || process.env.PIXEL_CONCURRENCY || 4);
const HUB_BIN = process.env.PIXEL_HUB_BIN || join(PIXEL_DIR, '.bin', 'hub');
const RUN_DIR = process.env.PIXEL_RUN || '/tmp/pixel-run';
const WIN = process.platform === 'win32';

// ---------------------------------------------------------------------------------------------- the hub
const toWsl = (p) => (WIN ? p.replace(/^([A-Za-z]):[\\/]/, (_, d) => `/mnt/${d.toLowerCase()}/`).replace(/\\/g, '/') : p);
function hubSh(cmd) {
  const script = toWsl(join(PIXEL_DIR, 'hub', 'hub.sh'));
  const env = [`PIXEL_HUB_BIN=${toWsl(HUB_BIN)}`, `PIXEL_PORT=${PORT}`, `PIXEL_RUN=${RUN_DIR}`, `PIXEL_HUB_STATIC=${toWsl(REPO_ROOT)}`, `PIXEL_PASSWORD=${PASSWORD}`];
  const [file, argv] = WIN ? ['wsl', ['-e', 'env', ...env, 'bash', script, cmd]] : ['bash', [script, cmd]];
  const r = spawnSync(file, argv, {encoding: 'utf8', env: WIN ? process.env : {...process.env, ...Object.fromEntries(env.map((e) => e.split(/=(.*)/s).slice(0, 2)))}});
  if (r.status !== 0) throw new Error(`hub.sh ${cmd} failed: ${(r.stdout || '') + (r.stderr || '')}`.slice(-1500));
}
function backdate() {
  const py = join(PIXEL_DIR, 'backdate.py');
  const argv = [WIN ? toWsl(py) : py, `${RUN_DIR}/data/hub.db`, String(Math.floor(ANCHOR.getTime() / 1000))];
  const [file, a] = WIN ? ['wsl', ['-e', 'python3', ...argv]] : ['python3', argv];
  const r = spawnSync(file, a, {encoding: 'utf8'});
  if (r.status !== 0) throw new Error(`backdate failed: ${r.stdout}${r.stderr}`);
}
// the hub answers inside WSL before Windows forwards the port, so wait from this side too
async function waitHub() {
  for (let i = 0; i < 120; i++) {
    try { if ((await fetch(`${HUB_URL}/api/healthz`)).ok) return; } catch { /* not forwarded yet */ }
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`the hub at ${HUB_URL} does not answer`);
}
async function startHub() {
  if (process.env.PIXEL_HUB_URL) {
    if (!existsSync(STATE_FILE)) await seed();
    return false;
  }
  if (!existsSync(HUB_BIN)) throw new Error(`no hub binary at ${HUB_BIN}: build one with tests/pixel/build-hub.sh or set PIXEL_HUB_BIN`);
  console.log('starting the hub and seeding it');
  hubSh('up');
  await waitHub();
  await seed();
  hubSh('stop');
  backdate();
  hubSh('start');
  await waitHub();
  return true;
}

// ---------------------------------------------------------------------------------------------- the boards
function boardHtml(name, theme) {
  const src = readFileSync(join(BOARDS_DIR, `${name}.dc.html`), 'utf8');
  const head = /<helmet>([\s\S]*?)<\/helmet>/.exec(src)[1].replace(/<link[^>]*fonts\.(googleapis|gstatic)\.com[^>]*>/g, '');
  const body = /<\/helmet>([\s\S]*)<\/x-dc>/.exec(src)[1].replaceAll('{{themeClass}}', theme === 'dark' ? 'dk' : 'lt');
  return `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/css/asc-fonts.css">${head}</head><body>${body}</body></html>`;
}
function boardServer() {
  const server = createServer((req, res) => {
    const url = new URL(req.url, 'http://x');
    try {
      let m;
      let type = 'text/html; charset=utf-8';
      let data;
      if ((m = /^\/board\/([\w-]+)$/.exec(url.pathname))) data = boardHtml(m[1], url.searchParams.get('theme'));
      else if (url.pathname === '/css/asc-fonts.css') { type = 'text/css'; data = readFileSync(join(REPO_ROOT, 'web_src/css/asc-fonts.css')); }
      else if ((m = /^\/fonts\/([\w.-]+\.woff2)$/.exec(url.pathname))) { type = 'font/woff2'; data = readFileSync(join(REPO_ROOT, 'web_src/fonts', m[1])); }
      if (data === undefined) { res.writeHead(404); res.end(); return; }
      res.writeHead(200, {'Content-Type': type});
      res.end(data);
    } catch (e) { console.error('board server:', url.pathname, String(e)); res.writeHead(500); res.end(); }
  });
  return new Promise((ok) => server.listen(0, '127.0.0.1', () => ok({server, base: `http://127.0.0.1:${server.address().port}`})));
}

// ---------------------------------------------------------------------------------------------- the run
// a page that never settles must not stall the run: after two minutes the job counts as a crashed browser
const guard = (p) => {
  let timer;
  const limit = new Promise((_, reject) => { timer = setTimeout(() => reject(new Error('job timeout, browser closed')), 120_000); });
  return Promise.race([p, limit]).finally(() => clearTimeout(timer));
};
const sleep = (ms) => new Promise((r) => setTimeout(r, ms));
async function login(browser, user) {
  const ctx = await browser.newContext({baseURL: HUB_URL});
  const page = await ctx.newPage();
  await page.goto('/user/login');
  await page.fill('#user_name', user);
  await page.fill('#password', PASSWORD);
  await Promise.all([page.waitForURL((u) => !u.pathname.startsWith('/user/login'), {timeout: 20000}), page.click('form[action="/user/login"] button.ui.primary.button, form[action="/user/login"] button[type=submit], form.ui.form button.primary')]);
  const st = await ctx.storageState();
  await ctx.close();
  return st;
}

function fill(route, state) {
  return route.replace(/\{(\w+)\}/g, (_, k) => (k === 'uid_priya' ? state.uid.priya : state[k] ?? k));
}

async function shot(page, url, {width, height, compareHeight, board}) {
  await page.setViewportSize({width, height});
  let status = 200;
  let finalUrl = url;
  for (let i = 0; ; i++) { // the first page of a fresh context can race the clock set-up
    try { await page.clock.setFixedTime(ANCHOR); break; } catch (err) { if (i >= 4) throw err; await sleep(300); }
  }
  const resp = await page.goto(url, {waitUntil: 'load', timeout: 30000});
  if (resp) { status = resp.status(); finalUrl = resp.url(); }
  try { await page.waitForLoadState('networkidle', {timeout: 5000}); } catch { /* polling pages never go idle */ }
  if (board) await page.evaluate(hideBoardStates);
  await page.evaluate(settle);
  await sleep(120);
  const m = await page.evaluate(measure);
  const fullH = Math.min(m.height, 8000);
  const clip = {x: 0, y: 0, width, height: compareHeight ? Math.min(compareHeight, fullH) : fullH};
  const png = await page.screenshot({fullPage: true, clip, animations: 'disabled', caret: 'hide'});
  return {png, m, status, finalUrl};
}

async function main() {
  const spec = JSON.parse(readFileSync(join(PIXEL_DIR, 'boards.json'), 'utf8'));
  let entries = spec.boards;
  if (only) entries = entries.filter((e) => only.includes(e.board));
  const mapped = entries.filter((e) => !e.skip);
  mkdirSync(join(OUT_DIR, 'shots'), {recursive: true});
  const ownHub = await startHub();
  const state = JSON.parse(readFileSync(STATE_FILE, 'utf8'));
  const {server, base} = await boardServer();
  const launch = () => chromium.launch({channel: process.env.PIXEL_BROWSER_CHANNEL || undefined, args: ['--hide-scrollbars', '--force-color-profile=srgb', '--font-render-hinting=none', '--disable-lcd-text']});
  let browser = await launch();
  const personas = [...new Set(mapped.map((e) => e.as))];
  const states = {};
  for (const p of personas) states[p] = p === 'anon' ? undefined : await login(browser, p);
  const contexts = new Map();
  let generation = 0;
  let resetting = null;
  // a crashed browser is replaced once, whatever number of jobs notice it
  const resetBrowser = (seen) => {
    if (seen !== generation) return resetting;
    generation++;
    contexts.clear();
    resetting = (async () => { try { await browser.close(); } catch { /* already gone */ } browser = await launch(); })();
    return resetting;
  };
  const ctxFor = async (key, options) => {
    if (!contexts.has(key)) contexts.set(key, browser.newContext({reducedMotion: 'reduce', locale: 'en-US', timezoneId: 'UTC', deviceScaleFactor: 1, serviceWorkers: 'block', ...options}));
    return contexts.get(key);
  };

  const jobs = [];
  for (const e of mapped) for (const theme of themes) if (e.themes.includes(theme)) jobs.push({e, theme});
  console.log(`${jobs.length} comparisons (${mapped.length} boards x ${themes.join('/')}), ${entries.length - mapped.length} unmapped`);
  const results = [];
  let done = 0;
  const run = async ({e, theme}) => {
    const id = `${e.board}__${theme}`;
    const vp = spec.viewports[e.viewport];
    const route = fill(e.route, state);
    const r = {id, board: e.board, num: e.num, title: e.title, route, resolvedRoute: route, theme, viewport: e.viewport, as: e.as, needsAsc: !!e.needsAsc, note: e.note || '', compareHeight: e.compareHeight || null};
    for (let attempt = 0; attempt < 3; attempt++) {
      const seen = generation;
      let lp, bp;
      try {
        const opts = {width: vp.width, height: vp.height, compareHeight: e.compareHeight};
        const ctx = await ctxFor(`live|${e.as}|${e.viewport}|${theme}`, {storageState: states[e.as], viewport: vp, colorScheme: theme});
        const bctx = await ctxFor(`board|${e.viewport}|${theme}`, {viewport: vp, colorScheme: theme});
        [lp, bp] = [await ctx.newPage(), await bctx.newPage()];
        const b = await guard(shot(bp, `${base}/board/${e.board}?theme=${theme}`, {...opts, board: true}));
        const l = await guard(shot(lp, `${HUB_URL}${route}`, opts));
        const px = await comparePixels(b.png, l.png, vp.width);
        const s = compareStructure(b.m, l.m);
        const d = deltas(b.m, l.m, s, l.status);
        for (const [name, buf] of [['board', b.png], ['live', l.png]]) {
          writeFileSync(join(OUT_DIR, 'shots', `${id}-${name}.webp`), await sharp(buf).webp({quality: 90}).toBuffer());
        }
        writeFileSync(join(OUT_DIR, 'shots', `${id}-diff.webp`), px.diffPng);
        Object.assign(r, {
          ok: true, status: l.status, finalPath: new URL(l.finalUrl).pathname + new URL(l.finalUrl).search, liveTitle: l.m.title, liveH1: l.m.h1, boardH1: b.m.h1,
          mismatch: px.mismatch, diffPixels: px.diffPixels, width: px.width, height: px.height, boardHeight: px.boardHeight, liveHeight: px.liveHeight,
          bands: px.bands.map((v) => Math.round(v * 1000) / 1000),
          structure: {boardCount: s.boardCount, liveCount: s.liveCount, matched: s.matched.length, missing: s.missing.length, extra: s.extra.length, coverage: s.coverage, byKind: s.byKind,
            missingList: s.missing.slice(0, 25).map((m) => ({kind: m.kind, text: m.text, x: m.x, y: m.y, w: m.w, h: m.h})),
            extraList: s.extra.slice(0, 15).map((m) => ({kind: m.kind, text: m.text, x: m.x, y: m.y, w: m.w, h: m.h}))},
          top: d.top.map((x) => x.text), deltas: d.all.map((x) => ({type: x.type, text: x.text, score: Math.round(x.score)})),
        });
        break;
      } catch (err) {
        const crashed = /closed|crash|Target/i.test(String(err));
        if (crashed && attempt < 2) { await resetBrowser(seen); continue; }
        Object.assign(r, {ok: false, error: String(err).slice(0, 400), mismatch: 1, top: [`harness error: ${String(err).slice(0, 160)}`]});
        break;
      } finally {
        await Promise.allSettled([lp, bp].filter(Boolean).map((pg) => Promise.race([pg.close(), sleep(10000)]))); // a page that will not close is left to the browser reset
      }
    }
    results.push(r);
    done++;
    if (done % 10 === 0 || done === jobs.length) console.log(`  ${done}/${jobs.length}`);
  };
  let next = 0;
  await Promise.all(Array.from({length: concurrency}, async () => { while (next < jobs.length) await run(jobs[next++]); }));

  await Promise.race([browser.close(), sleep(15000)]).catch(() => {});
  server.close();
  const skipped = entries.filter((e) => e.skip).map((e) => ({board: e.board, num: e.num, title: e.title, reason: e.skip}));
  results.sort((a, b) => b.mismatch - a.mismatch);
  const out = {generatedAt: new Date().toISOString(), anchor: ANCHOR.toISOString(), hub: HUB_URL, themes, results, skipped};
  writeFileSync(join(OUT_DIR, 'results.json'), JSON.stringify(out, null, 1));
  writeReport(OUT_DIR, out);
  console.log(`report: ${join(OUT_DIR, 'index.html')}`);
  if (ownHub && !args['keep-hub'] && !process.env.PIXEL_KEEP_HUB) hubSh('down');
}

main().then(() => process.exit(0), (e) => { console.error(e); process.exit(1); });
