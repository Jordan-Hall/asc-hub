// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {GET, POST} from '../modules/fetch.js';

// Small behaviours of the agent and trust boards (templates/repo/asc_agents*.tmpl, asc_queue.tmpl, asc_ops.tmpl).
// Every one is an enhancement: the page works as plain forms and links without this script. Text from the server is
// only ever assigned to textContent.

/** The decision a permission or plan form sends: the submit button's value, else the form's own field. */
export function decisionOf(form, submitter) {
  const v = submitter?.value || form.querySelector('[name=decision]')?.value || '';
  return ['allow', 'deny', 'approve', 'reject'].includes(v) ? v : '';
}

/** Draws the answer of a decision on the request card and the count of the panel. */
export function applyDecision(card, panel, answer) {
  const status = card.querySelector('[data-asc-need-status]');
  if (status) status.textContent = answer.message || '';
  if (!answer.ok) return false;
  card.classList.add('is-done');
  for (const b of card.querySelectorAll('button, a.rv-btn')) {
    b.setAttribute('disabled', '');
    b.setAttribute('aria-disabled', 'true');
  }
  const count = panel?.querySelector('[data-asc-needs-count]');
  if (count) {
    const n = Math.max(0, Number.parseInt(count.textContent, 10) - 1);
    count.textContent = String(Number.isNaN(n) ? 0 : n);
  }
  return true;
}

async function onDecide(e) {
  const form = e.target.closest?.('form[data-asc-decide]');
  if (!form) return;
  const decision = decisionOf(form, e.submitter);
  if (!decision) return;
  e.preventDefault();
  const card = form.closest('[data-asc-need]');
  const panel = form.closest('[data-asc-needs]');
  const buttons = form.querySelectorAll('button');
  for (const b of buttons) b.disabled = true;
  const body = new FormData(form);
  body.set('decision', decision);
  try {
    const res = await POST(form.action, {data: body, headers: {'X-Asc-Fetch': '1'}});
    const answer = await res.json().catch(() => ({ok: false, message: ''}));
    if (!applyDecision(card, panel, answer)) for (const b of buttons) b.disabled = false;
  } catch {
    applyDecision(card, panel, {ok: false, message: form.getAttribute('data-error') || ''});
    for (const b of buttons) b.disabled = false;
  }
}

/** Hides the rows of a list that do not contain the typed text. */
export function filterRows(list, text) {
  const needle = text.trim().toLowerCase();
  let shown = 0;
  for (const row of list.querySelectorAll('[data-asc-row]')) {
    // A row with a key (a file path) matches by its start; any other row by the text it shows.
    const key = row.getAttribute('data-asc-key');
    const hit = !needle || (key === null ? row.textContent.toLowerCase().includes(needle) : key.toLowerCase().startsWith(needle));
    row.hidden = !hit;
    if (hit) shown++;
  }
  return shown;
}

function onFilterInput(e) {
  const input = e.target.closest?.('input[data-asc-filter]');
  if (!input) return;
  const list = document.querySelector(input.getAttribute('data-asc-filter'));
  if (list) filterRows(list, input.value);
}

function onSelectChange(e) {
  const sel = e.target.closest?.('select[data-submit-on-change]');
  if (sel?.form) sel.form.requestSubmit();
}

/** Puts a recent task into the form. Only text is assigned; the textarea never parses it. */
export function reuseTask(button, doc = document) {
  const task = doc.querySelector('[data-asc-estimate-task]');
  if (!task) return false;
  task.value = button.getAttribute('data-asc-reuse') || '';
  const agent = button.getAttribute('data-asc-reuse-agent');
  const agentInput = doc.querySelector('[data-asc-estimate-agent]');
  if (agent && agentInput) agentInput.value = agent;
  task.dispatchEvent(new Event('input', {bubbles: true}));
  task.focus();
  return true;
}

/** Draws an estimate answer on its card. */
export function applyEstimate(card, est) {
  const known = card.querySelector('[data-est-known]');
  const unknown = card.querySelector('[data-est-unknown]');
  if (!known || !unknown) return;
  const ok = Boolean(est?.known);
  known.hidden = !ok;
  unknown.hidden = ok;
  if (!ok) return;
  card.querySelector('[data-est-duration]').textContent = String(est.duration ?? '');
  card.querySelector('[data-est-cost]').textContent = String(est.cost ?? '');
  card.querySelector('[data-est-samples]').textContent = String(Number(est.samples) || 0);
}

let estimateTimer = 0;
function onEstimateInput(e) {
  const t = e.target;
  if (!t.closest?.('[data-asc-estimate-task], [data-asc-estimate-agent]')) return;
  const card = document.querySelector('[data-asc-estimate]');
  if (!card) return;
  clearTimeout(estimateTimer);
  estimateTimer = setTimeout(async () => {
    const task = document.querySelector('[data-asc-estimate-task]');
    const agent = document.querySelector('[data-asc-estimate-agent]');
    const q = new URLSearchParams({agent: agent?.value.trim() || '', bytes: String(new TextEncoder().encode(task?.value || '').length)});
    try {
      const res = await GET(`${card.getAttribute('data-asc-estimate')}?${q}`, {headers: {Accept: 'application/json'}});
      if (res.ok) applyEstimate(card, await res.json());
    } catch { /* the estimate is a hint; the form still works */ }
  }, 600);
}

function onClick(e) {
  const b = e.target.closest?.('[data-asc-reuse]');
  if (b) reuseTask(b);
}

/** The row ids of a queue list, in order. */
export function queueIds(list) {
  return [...list.querySelectorAll('[data-q-change]')].map((li) => li.getAttribute('data-q-change'));
}

/** Draws a live answer on the list: the estimate lines and the median. Returns true when the set of entries changed. */
export function applyQueue(list, snap, doc = document) {
  const live = new Map((snap.entries || []).map((e) => [e.change, e]));
  const ids = queueIds(list);
  const gone = ids.some((id) => !live.has(id));
  const added = (snap.entries || []).some((e) => e.position > 0 && !ids.includes(e.change));
  for (const li of list.querySelectorAll('[data-q-change]')) {
    const e = live.get(li.getAttribute('data-q-change'));
    const slot = li.querySelector('[data-q-eta]');
    if (!e || !slot) continue;
    const parts = [e.detail, e.eta ? `about ${e.eta}` : e.startsIn ? `starts in ${e.startsIn}` : ''].filter(Boolean);
    slot.textContent = parts.join(', ');
    li.classList.toggle('is-checking', e.stage === 'checking' || e.stage === 'landing');
  }
  const median = doc.querySelector('[data-q-median]');
  if (median && snap.median) median.textContent = snap.median;
  return gone || added;
}

async function followQueue(list) {
  const url = list.getAttribute('data-asc-queue-live');
  let failures = 0;
  while (list.isConnected && failures < 5) {
    // The answer is held open until there is news, so this is only a floor against a server that answers at once.
    const started = Date.now();
    try {
      const res = await GET(url, {headers: {Accept: 'application/json'}});
      if (!res.ok) throw new Error(String(res.status));
      failures = 0;
      if (applyQueue(list, await res.json())) {
        window.location.reload();
        return;
      }
      const spent = Date.now() - started;
      if (spent < 1000) await new Promise((r) => setTimeout(r, 1000 - spent));
    } catch {
      failures++;
      await new Promise((r) => setTimeout(r, 2000 * failures));
    }
  }
}

export function initAscBoard() {
  const queue = document.querySelector('[data-asc-queue-live]');
  if (queue) followQueue(queue);
  document.addEventListener('click', onClick);
  document.addEventListener('input', onEstimateInput);
  document.addEventListener('submit', onDecide);
  document.addEventListener('input', onFilterInput);
  document.addEventListener('change', onSelectChange);
}
