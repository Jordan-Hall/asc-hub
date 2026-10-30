// The import form, progress page and failure page at 390 and 1440 px, light and dark, with axe (WCAG 2.2 AA).
// Needs the dev hub on :3100 with the mock GitHub (journey.sh). Screenshots: D:/tmp/fixbin/design-<page>-<w>-<scheme>.png
import { createRequire } from 'node:module';
import { execSync } from 'node:child_process';
const require = createRequire('D:/dev/asc-hub/package.json');
const { chromium } = require('playwright');
const AxeBuilder = require('@axe-core/playwright').default;
const b = await chromium.launch();
const results = [];
const sfx = String(Date.now() % 100000);
for (const scheme of ['light', 'dark']) {
  for (const width of [1440, 390]) {
    const ctx = await b.newContext({ viewport: { width, height: width === 390 ? 844 : 900 }, baseURL: 'http://localhost:3100', colorScheme: scheme });
    const p = await ctx.newPage();
    await p.goto('/user/login'); await p.fill('#user_name', 'Jordan'); await p.fill('#password', 'Passw0rd!Passw0rd');
    await Promise.all([p.waitForLoadState('load'), p.click('form button[type=submit]')]);
    const check = async (name) => {
      await p.screenshot({ path: `D:/tmp/fixbin/design-${name}-${width}-${scheme}.png`, fullPage: true });
      const axe = await new AxeBuilder({ page: p }).withTags(['wcag2a', 'wcag2aa', 'wcag21aa', 'wcag22aa']).analyze();
      const overflow = await p.evaluate(() => document.documentElement.scrollWidth > window.innerWidth + 1);
      results.push({ name, width, scheme, violations: axe.violations.map((v) => `${v.id} (${v.nodes.length}): ${v.nodes[0].target.join(' ')}`), overflow });
    };
    await p.goto('/repo/migrate?service_type=2'); await check('form-github');
    await p.goto('/repo/migrate?service_type=1'); await check('form-git');
    // a failed import: a source that does not exist
    const name = `dz${sfx}${width}${scheme}`.slice(0, 30);
    await p.goto('/repo/migrate?service_type=2');
    await p.fill('input[name=clone_addr]', 'http://127.0.0.1:8098/mock/broken'); await p.press('input[name=clone_addr]', 'Tab'); await p.waitForTimeout(500);
    await p.fill('input[name=auth_token]', 'tok'); await p.fill('input[name=repo_name]', ''); await p.fill('input[name=repo_name]', name);
    await Promise.all([p.waitForNavigation(), p.evaluate(() => document.querySelector('form[action$="/repo/migrate"]').requestSubmit())]);
    await check('progress');
    for (let i = 0; i < 30; i++) { await p.goto(`/Jordan/${name}`); if (await p.locator('#repo_migrating_retry:visible').count()) break; await p.waitForTimeout(1500); }
    await check('failed');
    await ctx.close();
  }
}
await b.close();
for (const r of results) console.log(JSON.stringify(r));
