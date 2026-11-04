// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {initAdminHealth, runHealthSteps} from './health.js';

function page() {
  document.body.innerHTML = `
    <span class="ds-chip ds-chip-ok" data-health-tally>2 passed, 0 warnings, 0 failed</span>
    <a href="/admin/self_check" data-health-run>Run again</a>
    <button data-health-report data-clipboard-text="old"></button>
    <ul data-health-steps="/admin/self_check/checks/" data-tally="{ok} passed, {warn} warnings, {fail} failed"
      data-tally-norun="{ok} passed, {warn} warnings, {fail} failed, {norun} could not run" data-waiting="Waiting"
      data-running="Running" data-norun="Could not run" data-norun-text="No answer." data-live-running="Checking {name}"
      data-live-done="Checks done: {tally}">
      ${['database', 'collation', 'cache'].map((id) => `<li data-check="${id}" data-state="ok"><span class="adm-tile adm-tile-ok"></span>
        <div data-check-name>${id}</div><div data-check-text>fine</div>
        <div class="adm-row-side"><span data-check-label>Passed</span><a href="/fix">Fix</a></div></li>`).join('')}
    </ul>
    <p data-health-live></p>`;
  return {list: document.querySelector('[data-health-steps]'), run: document.querySelector('[data-health-run]')};
}

function rowHTML(id, state, label, text) {
  return `<li data-check="${id}" data-state="${state}"><span class="adm-tile adm-tile-${state}"></span>
    <div data-check-name>${id}</div><div data-check-text>${text}</div>
    <div class="adm-row-side"><span data-check-label>${label}</span></div></li>`;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('the steps run one after the other and each row shows its own result', async () => {
  const {list, run} = page();
  const seen = [];
  vi.stubGlobal('fetch', vi.fn(async (url) => {
    // when a step runs, it is "Running" and the steps after it still wait
    seen.push(Array.from(list.querySelectorAll('[data-check]'), (r) => r.getAttribute('data-state')).join(','));
    if (url.endsWith('/database')) return new Response(rowHTML('database', 'ok', 'Passed', 'answers'), {status: 200});
    if (url.endsWith('/collation')) return new Response(null, {status: 204});
    return new Response(rowHTML('cache', 'warn', 'Warning', 'slow'), {status: 200});
  }));
  await runHealthSteps(document, list, run);
  expect(seen).toEqual(['run,wait,wait', 'ok,run,wait', 'ok,run']);
  expect(Array.from(list.querySelectorAll('[data-check]'), (r) => r.getAttribute('data-check'))).toEqual(['database', 'cache'], 'a step that does not apply is removed');
  expect(document.querySelector('[data-health-tally]').textContent).toBe('1 passed, 1 warnings, 0 failed');
  expect(document.querySelector('[data-health-tally]').className).toBe('ds-chip ds-chip-warn');
  expect(document.querySelector('[data-health-report]').getAttribute('data-clipboard-text')).toBe('database: Passed. answers\ncache: Warning. slow\n');
  expect(document.querySelector('[data-health-live]').textContent).toBe('Checks done: 1 passed, 1 warnings, 0 failed');
  expect(run.getAttribute('aria-disabled')).toBeNull();
  expect(list.getAttribute('aria-busy')).toBeNull();
});

test('a step that does not answer could not run', async () => {
  const {list, run} = page();
  vi.stubGlobal('fetch', vi.fn(async (url) => {
    if (url.endsWith('/database')) throw new TypeError('network');
    if (url.endsWith('/collation')) return new Response('oops', {status: 502});
    return new Response(rowHTML('cache', 'err', 'Failed', 'broken'), {status: 200});
  }));
  await runHealthSteps(document, list, run);
  const rows = Array.from(list.querySelectorAll('[data-check]'));
  expect(rows.map((r) => r.getAttribute('data-state'))).toEqual(['norun', 'norun', 'err']);
  expect(rows[0].querySelector('[data-check-label]').textContent).toBe('Could not run');
  expect(rows[0].querySelector('[data-check-text]').textContent).toBe('No answer.');
  expect(rows[0].querySelector('.adm-row-side a'), 'an old fix link is not kept').toBeNull();
  expect(document.querySelector('[data-health-tally]').textContent).toBe('0 passed, 0 warnings, 1 failed, 2 could not run');
  expect(document.querySelector('[data-health-tally]').className).toBe('ds-chip ds-chip-err');
});

test('the run link runs the steps in the page instead of loading it', async () => {
  const {run} = page();
  vi.stubGlobal('fetch', vi.fn(async () => new Response(null, {status: 204})));
  initAdminHealth();
  const click = new MouseEvent('click', {cancelable: true, bubbles: true});
  run.dispatchEvent(click);
  expect(click.defaultPrevented).toBe(true);
  expect(run.getAttribute('aria-disabled')).toBe('true');
  await vi.waitFor(() => expect(run.getAttribute('aria-disabled')).toBeNull());
  expect(fetch).toHaveBeenCalledWith('/admin/self_check/checks/database', expect.objectContaining({method: 'GET'}));
});
