// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {GET} from '../../modules/fetch.js';

// The run panel of the monitor page (board AdminMonitor): after "Run now" it follows the output of the run as the task
// writes it, then shows how it ended. It asks the hub for the lines after the last it has, once a second. Without the
// script the panel shows the output at the time the page loaded and a Refresh link.

const POLL_MS = 1000;
// A run that has not begun after this many polls is reported as not started.
const WAIT_POLLS = 15;
// The panel gives up after this many failed polls in a row.
const FAIL_POLLS = 5;

export function initAdminCronRun(root = document, delay = POLL_MS) {
  const panel = root.querySelector('[data-cron-output]');
  if (!panel) return null;
  const state = panel.getAttribute('data-state');
  if (state !== 'running' && state !== 'waiting') return null;
  return followCronRun(panel, delay);
}

function setState(panel, state) {
  panel.setAttribute('data-state', state);
  const chip = panel.querySelector('[data-cron-state]');
  if (!chip) return;
  chip.textContent = panel.getAttribute(`data-label-${state}`) ?? '';
  let tone = 'ds-chip-outline';
  if (state === 'finished') tone = 'ds-chip-ok';
  if (state === 'failed') tone = 'ds-chip-err';
  chip.className = `ds-chip ${tone}`;
}

function note(panel, text) {
  const empty = panel.querySelector('[data-cron-empty]');
  if (!empty) return;
  empty.textContent = text;
  empty.hidden = false;
}

// applyCronOutput puts one answer of the hub on the panel and returns whether the run is over.
export function applyCronOutput(panel, out) {
  const after = Number(panel.getAttribute('data-after') ?? 0);
  if (out.run <= after && !out.running) return false;
  const list = panel.querySelector('[data-cron-lines]');
  const empty = panel.querySelector('[data-cron-empty]');
  const took = panel.querySelector('[data-cron-took]');
  // When the run began after the page loaded, the lines on the panel are those of the last run.
  if (list && panel.getAttribute('data-state') === 'waiting') list.textContent = '';
  let next = Number(panel.getAttribute('data-next') ?? 0);
  if (out.from > next && list) list.textContent = '';
  const start = Math.max(0, next - out.from);
  for (const line of out.lines.slice(start)) {
    const li = document.createElement('li');
    li.textContent = line;
    list?.append(li);
  }
  next = out.next;
  panel.setAttribute('data-next', String(next));
  if (list && list.lastElementChild) list.scrollTop = list.scrollHeight;
  if (empty) empty.hidden = Boolean(list && list.childElementCount > 0);
  if (took) took.textContent = (panel.getAttribute('data-took') ?? '{s}').replace('{s}', out.seconds.toFixed(1));
  if (out.running) {
    setState(panel, 'running');
    return false;
  }
  setState(panel, out.failed ? 'failed' : 'finished');
  return true;
}

export function followCronRun(panel, delay = POLL_MS) {
  const url = panel.getAttribute('data-cron-output');
  let polls = 0;
  let fails = 0;
  return new Promise((resolve) => {
    const tick = async () => {
      polls++;
      try {
        const resp = await GET(`${url}?from=${encodeURIComponent(panel.getAttribute('data-next') ?? '0')}`);
        if (!resp.ok) throw new Error(`status ${resp.status}`);
        const out = await resp.json();
        fails = 0;
        if (applyCronOutput(panel, out)) {
          resolve('done');
          return;
        }
        if (panel.getAttribute('data-state') === 'waiting' && polls >= WAIT_POLLS) {
          note(panel, panel.getAttribute('data-stuck') ?? '');
          resolve('stuck');
          return;
        }
      } catch {
        fails++;
        if (fails >= FAIL_POLLS) {
          note(panel, panel.getAttribute('data-lost') ?? '');
          resolve('lost');
          return;
        }
      }
      setTimeout(tick, delay);
    };
    setTimeout(tick, 0);
  });
}
