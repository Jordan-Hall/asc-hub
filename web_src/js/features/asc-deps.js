// The dependencies page (repo/asc_deps). A finding opens in place: its Details button shows why it matters, how the
// package gets in and the fix. Without scripts every finding is drawn open and the buttons stay hidden. The Outdated tile
// asks the registries after the page is drawn (GET asc/deps/outdated), because that takes a while; a failure is said as
// such and never drawn as zero.

import {GET} from '../modules/fetch.js';

export function outdatedText(sub, counts) {
  const parts = [];
  if (counts.security > 0) parts.push(sub.getAttribute('data-security').replace('%d', String(counts.security)));
  if (counts.unknown > 0) parts.push(sub.getAttribute('data-unknown').replace('%d', String(counts.unknown)));
  return parts.join(' · ');
}

export function initDepToggle(button) {
  const detail = document.querySelector(`#${CSS.escape(button.getAttribute('aria-controls'))}`);
  if (!detail) return;
  detail.hidden = true;
  button.hidden = false;
  button.addEventListener('click', () => {
    const open = button.getAttribute('aria-expanded') !== 'true';
    button.setAttribute('aria-expanded', String(open));
    detail.hidden = !open;
  });
}

export async function loadOutdated(tile) {
  const value = tile.querySelector('[data-deps-outdated-n]');
  const sub = tile.querySelector('[data-deps-outdated-sub]');
  sub.textContent = sub.getAttribute('data-loading');
  try {
    const res = await GET(tile.getAttribute('data-url'));
    if (!res.ok) throw new Error(`outdated: ${res.status}`);
    const counts = await res.json();
    value.textContent = String(counts.outdated);
    sub.textContent = outdatedText(sub, counts);
  } catch {
    value.textContent = '–';
    sub.textContent = sub.getAttribute('data-failed');
  }
}

export function initAscDeps() {
  for (const button of document.querySelectorAll('[data-dep-toggle]')) initDepToggle(button);
  const tile = document.querySelector('[data-deps-outdated]');
  if (tile) loadOutdated(tile);
}
