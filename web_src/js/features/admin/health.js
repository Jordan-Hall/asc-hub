// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {GET} from '../../modules/fetch.js';

// "Run again" on the health page (board AdminHealth) runs the checks one after the other. Every row waits, then runs,
// then shows its result, so the person sees each step finish. A step that does not answer is "could not run". Without
// the script the button is a link that loads the page again.
export function initAdminHealth(root = document) {
  const list = root.querySelector('[data-health-steps]');
  const run = root.querySelector('[data-health-run]');
  if (!list || !run) return;
  run.addEventListener('click', (e) => {
    e.preventDefault();
    if (run.getAttribute('aria-disabled') === 'true') return;
    runHealthSteps(root, list, run);
  });
}

function setRowState(row, state, label) {
  row.setAttribute('data-state', state);
  const tile = row.querySelector('.adm-tile');
  if (tile) {
    tile.className = `adm-tile adm-tile-${state}`;
    tile.textContent = '';
  }
  const chip = row.querySelector('[data-check-label]');
  if (chip) {
    chip.className = 'ds-chip ds-chip-outline';
    chip.textContent = label;
  }
  for (const fix of row.querySelectorAll('.adm-row-side a')) fix.remove();
}

function fill(format, values) {
  return format.replace(/\{(\w+)\}/g, (all, k) => (k in values ? String(values[k]) : all));
}

export async function runHealthSteps(root, list, run) {
  const live = root.querySelector('[data-health-live]');
  const tally = root.querySelector('[data-health-tally]');
  const report = root.querySelector('[data-health-report]');
  const rows = Array.from(list.querySelectorAll('[data-check]'));
  run.setAttribute('aria-disabled', 'true');
  run.classList.add('is-loading');
  list.setAttribute('aria-busy', 'true');
  for (const row of rows) {
    setRowState(row, 'wait', list.getAttribute('data-waiting'));
    const text = row.querySelector('[data-check-text]');
    if (text) text.textContent = '';
  }
  for (const row of rows) {
    const name = row.querySelector('[data-check-name]')?.textContent.trim() ?? '';
    setRowState(row, 'run', list.getAttribute('data-running'));
    if (live) live.textContent = fill(list.getAttribute('data-live-running'), {name});
    let next = null;
    try {
      const resp = await GET(`${list.getAttribute('data-health-steps')}${encodeURIComponent(row.getAttribute('data-check'))}`);
      if (resp.status === 204) {
        row.remove();
        continue;
      }
      if (resp.ok) {
        const doc = new DOMParser().parseFromString(await resp.text(), 'text/html');
        next = doc.querySelector(`[data-check="${CSS.escape(row.getAttribute('data-check'))}"]`);
      }
    } catch {
      next = null;
    }
    if (next) {
      row.replaceWith(document.importNode(next, true));
    } else {
      setRowState(row, 'norun', list.getAttribute('data-norun'));
      const text = row.querySelector('[data-check-text]');
      if (text) text.textContent = list.getAttribute('data-norun-text');
    }
  }
  const counts = {ok: 0, warn: 0, fail: 0, norun: 0};
  const lines = [];
  for (const row of list.querySelectorAll('[data-check]')) {
    const state = row.getAttribute('data-state');
    if (state === 'ok') counts.ok++;
    else if (state === 'warn') counts.warn++;
    else if (state === 'norun') counts.norun++;
    else counts.fail++;
    const part = (sel) => row.querySelector(sel)?.textContent.trim() ?? '';
    lines.push(`${part('[data-check-name]')}: ${part('[data-check-label]')}. ${part('[data-check-text]')}\n`);
  }
  const summary = fill(list.getAttribute(counts.norun ? 'data-tally-norun' : 'data-tally'), counts);
  if (tally) {
    tally.textContent = summary;
    const tone = counts.fail ? 'err' : (counts.warn || counts.norun ? 'warn' : 'ok');
    tally.className = `ds-chip ds-chip-${tone}`;
  }
  if (report) report.setAttribute('data-clipboard-text', lines.join(''));
  if (live) live.textContent = fill(list.getAttribute('data-live-done'), {tally: summary});
  list.removeAttribute('aria-busy');
  run.removeAttribute('aria-disabled');
  run.classList.remove('is-loading');
}
