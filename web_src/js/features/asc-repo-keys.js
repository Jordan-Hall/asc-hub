// Keyboard shortcuts of the repository pages (boards Code, History, Stacks). The page declares what a key does:
// an element with data-asc-key="t" (a link or a button) is clicked when t is pressed, and data-asc-key="g c" is a
// two-key sequence. A row of a list (data-asc-list > data-asc-row, moved with j and k by asc-keys.js) opens with
// Enter (its data-asc-open link, or its first link) and copies its id (data-asc-id) with c. Every shortcut is
// ignored while a field has the focus or a modifier key is held, and each one belongs to a visible control, so
// nothing depends on the keyboard.

import {isTypingTarget} from './asc-keys.js';

export const chordMs = 1200;

export function splitKeys(value) {
  return String(value || '').trim().split(/\s+/).filter(Boolean);
}

// Reads the declared shortcuts: a Map of "t" or "g c" to the first element that declares it and is shown.
export function readKeymap(root = document) {
  const map = new Map();
  for (const el of root.querySelectorAll('[data-asc-key]')) {
    if (el.hidden || el.closest('[hidden]') || el.hasAttribute('disabled') || el.getAttribute('aria-disabled') === 'true') continue;
    const seq = splitKeys(el.getAttribute('data-asc-key')).join(' ');
    if (seq && !map.has(seq)) map.set(seq, el);
  }
  return map;
}

// One key press. state is {pending, at} from the last call; returns {seq, state}: seq is the shortcut that fired
// (or null) and state is what to pass to the next call.
export function resolveKey(state, key, now, seqs) {
  const pending = state.pending && now - state.at <= chordMs ? state.pending : null;
  if (pending && seqs.has(`${pending} ${key}`)) return {seq: `${pending} ${key}`, state: {pending: null, at: 0}};
  for (const seq of seqs) {
    if (seq.startsWith(`${key} `)) return {seq: null, state: {pending: key, at: now}};
  }
  if (seqs.has(key)) return {seq: key, state: {pending: null, at: 0}};
  return {seq: null, state: {pending: null, at: 0}};
}

function rowOf(el) {
  return el instanceof Element ? el.closest('[data-asc-list] [data-asc-row]') : null;
}

export function openRow(row) {
  const link = row.querySelector('[data-asc-open]') || row.querySelector('a[href]');
  if (link) link.click();
  return Boolean(link);
}

export async function copyRowId(row) {
  const id = row.getAttribute('data-asc-id');
  if (!id) return false;
  try {
    await navigator.clipboard.writeText(id);
  } catch {
    return false;
  }
  row.setAttribute('data-asc-copied', 'true');
  setTimeout(() => row.removeAttribute('data-asc-copied'), 1500);
  return true;
}

// A button with data-asc-toggle="class" and data-asc-toggle-target="selector" switches that class on the target and
// keeps aria-pressed in step. It returns the new state, or null when the button names no target.
export function toggleTarget(btn) {
  const target = document.querySelector(btn.getAttribute('data-asc-toggle-target') || '');
  const name = btn.getAttribute('data-asc-toggle');
  if (!target || !name) return null;
  const on = target.classList.toggle(name);
  btn.setAttribute('aria-pressed', String(on));
  return on;
}

let keyHandler = null;
let clickHandler = null;

export function initAscRepoKeys() {
  if (keyHandler) document.removeEventListener('keydown', keyHandler);
  if (clickHandler) document.removeEventListener('click', clickHandler);
  clickHandler = (e) => {
    const btn = e.target instanceof Element ? e.target.closest('[data-asc-toggle]') : null;
    if (btn) toggleTarget(btn);
  };
  document.addEventListener('click', clickHandler);
  let state = {pending: null, at: 0};
  keyHandler = (e) => {
    if (e.defaultPrevented || e.altKey || e.ctrlKey || e.metaKey) return;
    if (isTypingTarget(e.target)) return;
    const row = rowOf(document.activeElement);
    if (row && e.key === 'Enter' && document.activeElement === row) {
      if (openRow(row)) e.preventDefault();
      return;
    }
    if (row && e.key === 'c') {
      e.preventDefault();
      copyRowId(row);
      return;
    }
    const map = readKeymap();
    if (!map.size) return;
    const next = resolveKey(state, e.key, Date.now(), new Set(map.keys()));
    state = next.state;
    if (!next.seq) return;
    e.preventDefault();
    map.get(next.seq).click();
  };
  document.addEventListener('keydown', keyHandler);
}
