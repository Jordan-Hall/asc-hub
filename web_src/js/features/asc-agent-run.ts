// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {GET} from '../modules/fetch.js';

// The live pages of the agents and workflows area (templates/repo/asc_agents_run.tmpl, asc_workflows_run.tmpl):
//
//  - an agent run follows its event stream by polling `GET .../runs/{id}/events?after=N`, which holds the request
//    open until there is news (routers/web/repo/asc_agents_feed.go). The answer carries the new events, the state,
//    and the budget meters; this script draws them in place.
//  - a workflow run polls `GET .../runs/{id}/state` every few seconds and updates the pills of its steps.
//
// Everything the server sends is untrusted text. It is only ever assigned to textContent or to a property that
// takes a plain value, never to innerHTML, and links are taken from the server's own `changeUrl` only when it is
// a path on this site. Without this script the pages still show what existed when they were drawn.

interface Meter {
  key: string;
  used: string;
  limit: string;
  percent: number;
  unlimited: boolean;
  warn: boolean;
  over: boolean;
}

interface AgentEvent {
  seq: number;
  clock?: string;
  iso?: string;
  kind: string;
  word: string;
  tone: string;
  quiet: boolean;
  text?: string;
  tool?: string;
  change?: string;
  changeUrl?: string;
}

interface AgentFeed {
  state: string;
  label: string;
  tone: string;
  active: boolean;
  waiting: boolean;
  title: string;
  meters: Meter[];
  changes: number;
  failure?: string;
  events: AgentEvent[];
  last: number;
  done: boolean;
}

interface WorkflowStep {
  id: string;
  state: string;
  label: string;
  tone: string;
  error?: string;
}

interface WorkflowState {
  state: string;
  label: string;
  tone: string;
  active: boolean;
  error?: string;
  steps: WorkflowStep[];
  meters: Meter[];
}

const maxLogRows = 1000;
const maxBackoffMs = 30_000;
const maxFailures = 6;
const workflowPollMs = 3000;
const tones = new Set(['ok', 'bad', 'run', 'wait', 'warn', 'idle']);

function tone(value: string): string {
  return tones.has(value) ? value : 'idle';
}

function isLocalPath(url: string | undefined): url is string {
  return typeof url === 'string' && url.startsWith('/') && !url.startsWith('//') && !url.includes('\\');
}

function setPill(container: Element | null, state: string, label: string, toneName: string) {
  const pill = container?.querySelector('.asc-pill');
  if (!pill) return;
  pill.className = `asc-pill asc-tone-${tone(toneName)}`;
  pill.setAttribute('data-state', state.replace(/[^a-z]/g, ''));
  pill.textContent = label;
}

function setHidden(el: Element | null, hidden: boolean) {
  el?.classList.toggle('tw-hidden', hidden);
}

function updateMeters(root: HTMLElement, meters: Meter[] | undefined) {
  for (const m of meters ?? []) {
    const el = root.querySelector(`[data-meter="${CSS.escape(m.key)}"]`);
    if (!el) continue;
    const text = el.querySelector('[data-meter-text]');
    if (text) text.textContent = m.unlimited ? `${m.used} (no limit)` : `${m.used} of ${m.limit}`;
    const bar = el.querySelector('progress');
    if (bar) {
      bar.value = Math.max(0, Math.min(100, Number(m.percent) || 0));
      bar.classList.toggle('is-over', Boolean(m.over));
      bar.classList.toggle('is-warn', Boolean(m.warn) && !m.over);
    }
  }
}

function eventRow(ev: AgentEvent): HTMLLIElement {
  const li = document.createElement('li');
  li.className = `asc-log-row${ev.quiet ? ' is-quiet' : ''}`;
  li.setAttribute('data-seq', String(Number(ev.seq) || 0));
  li.setAttribute('data-kind', String(ev.kind ?? '').replace(/[^a-z_]/g, ''));

  const time = document.createElement('span');
  time.className = 'asc-log-time';
  if (ev.iso && ev.clock) {
    const t = document.createElement('time');
    t.setAttribute('datetime', ev.iso);
    t.textContent = ev.clock;
    time.append(t);
  }

  const kind = document.createElement('span');
  kind.className = `asc-log-kind asc-tone-${tone(ev.tone)}`;
  kind.textContent = ev.word;

  const body = document.createElement('span');
  body.className = 'asc-log-body';
  if (ev.tool) {
    const code = document.createElement('code');
    code.className = 'asc-log-tool';
    code.textContent = ev.tool;
    body.append(code);
  }
  if (ev.change) {
    if (isLocalPath(ev.changeUrl)) {
      const a = document.createElement('a');
      a.className = 'asc-log-change';
      a.href = ev.changeUrl;
      a.textContent = `change ${ev.change}`;
      body.append(a);
    } else {
      const code = document.createElement('code');
      code.className = 'asc-log-change';
      code.textContent = `change ${ev.change}`;
      body.append(code);
    }
  }
  if (ev.text) {
    const text = document.createElement('span');
    text.className = 'asc-log-text';
    text.textContent = ev.text;
    body.append(text);
  }
  li.append(time, kind, body);
  return li;
}

function appendEvents(root: HTMLElement, events: AgentEvent[]) {
  const log = root.querySelector('[data-log]');
  if (!log || events.length === 0) return;
  const frag = document.createDocumentFragment();
  for (const ev of events) frag.append(eventRow(ev));
  log.append(frag);
  while (log.children.length > maxLogRows) log.firstElementChild?.remove();
  setHidden(root.querySelector('[data-log-empty]'), true);
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function whenVisible(): Promise<void> {
  if (!document.hidden) return Promise.resolve();
  return new Promise((resolve) => {
    const onChange = () => {
      if (!document.hidden) {
        document.removeEventListener('visibilitychange', onChange);
        resolve();
      }
    };
    document.addEventListener('visibilitychange', onChange);
  });
}

function applyAgentFeed(root: HTMLElement, feed: AgentFeed) {
  setPill(root.querySelector('[data-run-state]'), feed.state, feed.label, feed.tone);
  const title = root.querySelector('[data-run-title]');
  if (title && feed.title) title.textContent = feed.title;
  const failure = root.querySelector('[data-run-failure]');
  if (failure) {
    failure.textContent = feed.failure ?? '';
    setHidden(failure, !feed.failure);
  }
  setHidden(root.querySelector('[data-run-waiting]'), !feed.waiting);
  updateMeters(root, feed.meters);
  appendEvents(root, feed.events ?? []);
  if (!feed.active) {
    root.querySelector('[data-run-cancel]')?.setAttribute('hidden', '');
    root.querySelector('[data-run-steer]')?.setAttribute('hidden', '');
  }
}

async function followAgentRun(root: HTMLElement) {
  const url = root.getAttribute('data-poll-url');
  const status = root.querySelector('[data-feed-status]');
  if (!url || !isLocalPath(url) || root.getAttribute('data-done') === 'true' || root.getAttribute('data-active') !== 'true') {
    return;
  }
  let last = Number(root.getAttribute('data-last-seq')) || 0;
  let failures = 0;
  while (root.isConnected) {
    await whenVisible();
    try {
      const resp = await GET(`${url}?after=${last}`, {headers: {Accept: 'application/json'}, credentials: 'same-origin'});
      if (!resp.ok) throw new Error(`status ${resp.status}`);
      const feed: AgentFeed = await resp.json();
      failures = 0;
      root.querySelector('[data-reconnect]')?.classList.add('tw-hidden');
      if (Number(feed.last) > last) last = Number(feed.last);
      applyAgentFeed(root, feed);
      if (feed.done) {
        if (status) status.textContent = 'The run is over; this is the whole log.';
        // The result stack, the buttons and the review links are drawn by the server: show them.
        window.location.reload();
        return;
      }
      if (!feed.active && feed.events.length === 0) {
        // Finished, and the server has nothing left to send but has not closed the stream yet.
        await sleep(1000);
      }
    } catch {
      failures++;
      if (failures >= maxFailures) {
        if (status) status.textContent = 'The connection to the hub was lost. Reload the page to follow the run.';
        return;
      }
      if (status) status.textContent = 'Reconnecting…';
      root.querySelector('[data-reconnect]')?.classList.remove('tw-hidden');
      await sleep(Math.min(maxBackoffMs, 1000 * 2 ** failures));
    }
  }
}

function applyWorkflowState(root: HTMLElement, st: WorkflowState): boolean {
  setPill(root.querySelector('[data-run-state]'), st.state, st.label, st.tone);
  const failure = root.querySelector('[data-run-failure]');
  if (failure) {
    failure.textContent = st.error ?? '';
    setHidden(failure, !st.error);
  }
  const rows = root.querySelectorAll('li[data-step]');
  if (rows.length !== (st.steps ?? []).length) return true; // steps started: the server draws them
  for (const step of st.steps ?? []) {
    const row = root.querySelector(`li[data-step="${CSS.escape(step.id)}"]`);
    if (!row) return true;
    setPill(row.querySelector('[data-step-state]'), step.state, step.label, step.tone);
    const err = row.querySelector('[data-step-error]');
    if (err) {
      err.textContent = step.error ?? '';
      setHidden(err, !step.error);
    }
  }
  updateMeters(root, st.meters);
  return false;
}

async function followWorkflowRun(root: HTMLElement) {
  const url = root.getAttribute('data-poll-url');
  if (!url || !isLocalPath(url) || root.getAttribute('data-active') !== 'true') return;
  let failures = 0;
  while (root.isConnected) {
    await sleep(workflowPollMs);
    await whenVisible();
    try {
      const resp = await GET(url, {headers: {Accept: 'application/json'}, credentials: 'same-origin'});
      if (!resp.ok) throw new Error(`status ${resp.status}`);
      const st: WorkflowState = await resp.json();
      failures = 0;
      const changed = applyWorkflowState(root, st);
      // A new step, or the end of the run: the server draws the logs, approvals and buttons for it.
      if (changed || !st.active) {
        window.location.reload();
        return;
      }
    } catch {
      failures++;
      if (failures >= maxFailures) return;
      await sleep(Math.min(maxBackoffMs, 1000 * 2 ** failures));
    }
  }
}

function initModelPicker() {
  const select = document.querySelector<HTMLSelectElement>('#asc-ag-key');
  const input = document.querySelector<HTMLInputElement>('#asc-ag-model');
  const list = document.querySelector<HTMLDataListElement>('#asc-ag-model-list');
  if (!select || !input || !list) return;
  let models: Record<string, unknown> = {};
  try {
    const parsed = JSON.parse(select.getAttribute('data-asc-models') || '{}');
    if (parsed && typeof parsed === 'object') models = parsed;
  } catch {
    models = {};
  }
  const fill = () => {
    list.replaceChildren();
    const ids = models[select.value];
    if (Array.isArray(ids)) {
      for (const id of ids) {
        if (typeof id !== 'string') continue;
        const opt = document.createElement('option');
        opt.value = id;
        list.append(opt);
      }
    }
    const def = select.selectedOptions[0]?.getAttribute('data-model');
    input.placeholder = def ? `default: ${def}` : "the key's default";
  };
  select.addEventListener('change', fill);
  fill();
}

export function initAscAgentRun() {
  for (const root of document.querySelectorAll<HTMLElement>('[data-asc-agent-run]')) {
    const toggle = root.querySelector<HTMLInputElement>('[data-show-all]');
    toggle?.addEventListener('change', () => root.classList.toggle('is-everything', toggle.checked));
    followAgentRun(root);
  }
  for (const root of document.querySelectorAll<HTMLElement>('[data-asc-workflow-run]')) {
    followWorkflowRun(root);
  }
  initModelPicker();
  for (const el of document.querySelectorAll<HTMLInputElement>('[data-asc-select]')) {
    el.addEventListener('focus', () => el.select());
  }
}
