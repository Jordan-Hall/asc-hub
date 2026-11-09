// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {initAdminCronRun} from './cronrun.js';

function page(state, {after = 0, next = 0, lines = []} = {}) {
  document.body.innerHTML = `
    <section data-cron-output="/admin/monitor/cron/verify_asc_audit_chain/output" data-state="${state}" data-after="${after}" data-next="${next}"
      data-label-waiting="Starting" data-label-running="Running" data-label-finished="OK" data-label-failed="Failed"
      data-took="{s} s" data-stuck="Not started." data-lost="Lost contact.">
      <span class="ds-chip ds-chip-outline" data-cron-state>${state}</span>
      <span data-cron-took></span>
      <ol data-cron-lines>${lines.map((l) => `<li>${l}</li>`).join('')}</ol>
      <p data-cron-empty ${lines.length ? 'hidden' : ''}>No output.</p>
    </section>`;
  return document.querySelector('[data-cron-output]');
}

const lines = (panel) => Array.from(panel.querySelectorAll('[data-cron-lines] li'), (li) => li.textContent);

function answers(list) {
  const urls = [];
  vi.stubGlobal('fetch', vi.fn(async (url) => {
    urls.push(String(url));
    const next = list.length > 1 ? list.shift() : list[0];
    if (next instanceof Error) throw next;
    if (typeof next === 'number') return new Response('', {status: next});
    return new Response(JSON.stringify(next), {status: 200, headers: {'Content-Type': 'application/json'}});
  }));
  return urls;
}

afterEach(() => {
  vi.unstubAllGlobals();
});

test('the panel follows the lines of a running task until it ends', async () => {
  const panel = page('running', {next: 1, lines: ['checked events 1 to 50000 of 90000']});
  const urls = answers([
    {run: 1, running: true, failed: false, seconds: 1.25, from: 1, next: 2, lines: ['checked events 50001 to 60000']},
    {run: 1, running: true, failed: false, seconds: 2, from: 2, next: 2, lines: []},
    {run: 1, running: false, failed: false, seconds: 2.5, from: 2, next: 3, lines: ['the chain is intact']},
  ]);
  expect(await initAdminCronRun(document, 0)).toEqual('done');
  expect(urls).toEqual([
    '/admin/monitor/cron/verify_asc_audit_chain/output?from=1',
    '/admin/monitor/cron/verify_asc_audit_chain/output?from=2',
    '/admin/monitor/cron/verify_asc_audit_chain/output?from=2',
  ]);
  expect(lines(panel)).toEqual(['checked events 1 to 50000 of 90000', 'checked events 50001 to 60000', 'the chain is intact']);
  expect(panel.getAttribute('data-state')).toEqual('finished');
  expect(panel.querySelector('[data-cron-state]').textContent).toEqual('OK');
  expect(panel.querySelector('[data-cron-state]').className).toContain('ds-chip-ok');
  expect(panel.querySelector('[data-cron-took]').textContent).toEqual('2.5 s');
  expect(panel.querySelector('[data-cron-empty]').hidden).toBe(true);
});

test('a waiting panel ignores the last run and shows the new one when it begins, and a failure', async () => {
  const panel = page('waiting', {after: 4});
  answers([
    {run: 4, running: false, failed: false, seconds: 3, from: 0, next: 2, lines: ['old', 'old']},
    {run: 5, running: true, failed: false, seconds: 0.1, from: 0, next: 1, lines: ['new']},
    {run: 5, running: false, failed: true, seconds: 0.4, from: 1, next: 2, lines: ['error: boom']},
  ]);
  expect(await initAdminCronRun(document, 0)).toEqual('done');
  expect(lines(panel)).toEqual(['new', 'error: boom']);
  expect(panel.getAttribute('data-state')).toEqual('failed');
  expect(panel.querySelector('[data-cron-state]').className).toContain('ds-chip-err');
});

test('lines dropped by the hub restart the list', async () => {
  const panel = page('running', {next: 2, lines: ['a', 'b']});
  answers([{run: 1, running: false, failed: false, seconds: 1, from: 520, next: 521, lines: ['z']}]);
  await initAdminCronRun(document, 0);
  expect(lines(panel)).toEqual(['z']);
});

test('a run that never begins, and a hub that stops answering, are said', async () => {
  let panel = page('waiting', {after: 2});
  answers([{run: 2, running: false, failed: false, seconds: 1, from: 0, next: 0, lines: []}]);
  expect(await initAdminCronRun(document, 0)).toEqual('stuck');
  expect(panel.querySelector('[data-cron-empty]').textContent).toEqual('Not started.');

  panel = page('running', {next: 0});
  answers([new Error('offline'), 502]);
  expect(await initAdminCronRun(document, 0)).toEqual('lost');
  expect(panel.querySelector('[data-cron-empty]').textContent).toEqual('Lost contact.');
  expect(panel.querySelector('[data-cron-empty]').hidden).toBe(false);
});

test('a finished panel does not poll', () => {
  page('finished');
  const urls = answers([{}]);
  expect(initAdminCronRun(document, 0)).toBeNull();
  expect(urls).toEqual([]);
  document.body.innerHTML = '';
  expect(initAdminCronRun(document, 0)).toBeNull();
});
