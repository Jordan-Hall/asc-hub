// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {POST} from '../modules/fetch.js';

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
    const hit = !needle || row.textContent.toLowerCase().includes(needle);
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

export function initAscBoard() {
  document.addEventListener('submit', onDecide);
  document.addEventListener('input', onFilterInput);
  document.addEventListener('change', onSelectChange);
}
