// The import journey in a real browser, on a hub with the mock GitHub (mock_github.py on :8098, repositories served on :8099):
// the form (GitHub address, token, options), the progress page, a failed import with its reason and Retry, a retried import
// that succeeds, and the imported repository. Screenshots go to D:/tmp/fixbin/journey-*.png.
import { createRequire } from 'node:module';
import { execSync } from 'node:child_process';
const require = createRequire('D:/dev/asc-hub/package.json');
const { chromium } = require('playwright');
const out = 'D:/tmp/fixbin/journey';
const b = await chromium.launch();
const p = await (await b.newContext({ viewport: { width: 1440, height: 900 }, baseURL: 'http://localhost:3100' })).newPage();
await p.goto('/user/login'); await p.fill('#user_name', 'Jordan'); await p.fill('#password', 'Passw0rd!Passw0rd');
await Promise.all([p.waitForLoadState('load'), p.click('form button[type=submit]')]);

async function migrate(name, source) {
  await p.goto('/repo/migrate?service_type=2');
  await p.fill('input[name=clone_addr]', `http://127.0.0.1:8098/mock/${source}`);
  await p.press('input[name=clone_addr]', 'Tab');
  await p.waitForTimeout(600);
  await p.fill('input[name=auth_token]', 'mock-token');
  for (const o of ['milestones', 'labels', 'releases', 'lfs']) {
    const box = p.locator(`input[name=${o}]`);
    if (await box.count()) await box.check({ force: true });
  }
  await p.fill('input[name=repo_name]', '');
  await p.fill('input[name=repo_name]', name);
  const lfsEp = p.locator('input[name=lfs_endpoint]');
  if (await lfsEp.count()) await lfsEp.fill('http://127.0.0.1:8098/lfs');
  await p.screenshot({ path: `${out}-1-form-${name}.png`, fullPage: true });
  await Promise.all([p.waitForNavigation(), p.evaluate(() => document.querySelector('form[action$="/repo/migrate"]').requestSubmit())]);
}

// 1. success on the first try
await migrate('jsuccess', 'proj');
await p.screenshot({ path: `${out}-2-progress-jsuccess.png` });
for (let i = 0; i < 60; i++) { await p.goto('/Jordan/jsuccess'); if (!(await p.locator('#repo_migrating').count())) break; await p.waitForTimeout(3000); }
await p.screenshot({ path: `${out}-3-success-jsuccess.png` });
console.log('success page:', p.url(), (await p.title()));
await p.goto('/Jordan/jsuccess/releases'); await p.screenshot({ path: `${out}-3b-releases-jsuccess.png` });
console.log('releases:', (await p.locator('li.rp-rel, .release-list-title, .rp-rel-title').allInnerTexts()).slice(0, 4).join(' | '));
await p.goto('/Jordan/jsuccess/milestones'); console.log('milestones text has v1 goals:', /v1 goals/.test(await p.locator('body').innerText()));

// 2. a failure (the source repository does not exist yet), its reason, Retry, then success
execSync('wsl.exe -d Ubuntu -- rm -rf /tmp/src-late.git', { stdio: 'ignore' });
await migrate('jlate', 'late');
let failed = false;
for (let i = 0; i < 40 && !failed; i++) { await p.goto('/Jordan/jlate'); failed = (await p.locator('#repo_migrating_retry:visible').count()) > 0; if (!failed) await p.waitForTimeout(2000); }
console.log('failed state shown:', failed, '|', (await p.locator('#repo_migrating_failed_error').textContent().catch(() => ''))?.trim().slice(0, 200));
await p.screenshot({ path: `${out}-4-failed-jlate.png` });
execSync('wsl.exe -d Ubuntu -- bash -c "cp -r /tmp/src-rel.git /tmp/src-late.git && chmod -R a+rX /tmp/src-late.git"', { stdio: 'ignore' });
await p.locator('#repo_migrating_retry').click();
for (let i = 0; i < 60; i++) { await p.waitForTimeout(3000); await p.goto('/Jordan/jlate'); if (!(await p.locator('#repo_migrating').count())) break; }
await p.screenshot({ path: `${out}-5-retried-jlate.png` });
console.log('after retry:', p.url(), await p.title());
await b.close();
