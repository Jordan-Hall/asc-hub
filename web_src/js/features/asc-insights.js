// The Insights board of an asc repository (repo/insights_asc). The Everyone / People / Agents and period controls are links
// that work without scripts; with scripts they load the board in place: the tiles and the chart give way to a skeleton
// while the new numbers load, then the whole board is swapped so the chart and the tiles change together. A failed load
// falls back to opening the link, which draws the server's own error state.

import {GET} from '../modules/fetch.js';

let current = null;

function plainClick(e) {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey;
}

function showLoading(board) {
  const body = board.querySelector('[data-ins-body]');
  const tpl = board.querySelector('template[data-ins-skeleton]');
  if (body && tpl) body.replaceChildren(tpl.content.cloneNode(true));
  board.setAttribute('aria-busy', 'true');
  const status = board.querySelector('[data-ins-status]');
  if (status) status.textContent = status.getAttribute('data-loading');
}

export async function loadInsights(board, url, push) {
  current?.abort();
  const ctl = new AbortController();
  current = ctl;
  showLoading(board);
  let next = null;
  try {
    const res = await GET(url, {signal: ctl.signal});
    if (res.ok) {
      const doc = new DOMParser().parseFromString(await res.text(), 'text/html');
      next = doc.querySelector('[data-ins-board]');
    }
  } catch (err) {
    if (err.name === 'AbortError') return;
  }
  if (ctl.signal.aborted) return;
  current = null;
  if (!next) {
    window.location.assign(url);
    return;
  }
  const active = document.activeElement?.getAttribute('href');
  board.replaceWith(next);
  initInsightsBoard(next);
  if (push) window.history.pushState({ascInsights: true}, '', url);
  if (active) {
    for (const a of next.querySelectorAll('[data-ins-segs] a')) {
      if (a.getAttribute('href') === active) a.focus();
    }
  }
}

export function initInsightsBoard(board) {
  const segs = board.querySelector('[data-ins-segs]');
  if (!segs) return;
  segs.addEventListener('click', (e) => {
    const a = e.target.closest('a[href]');
    if (!a || !plainClick(e)) return;
    e.preventDefault();
    loadInsights(board, a.getAttribute('href'), true);
  });
}

function onPopState(e) {
  const board = document.querySelector('[data-ins-board]');
  if (board && e.state?.ascInsights) loadInsights(board, window.location.href, false);
}

export function initAscInsights() {
  const board = document.querySelector('[data-ins-board]');
  if (!board) return;
  window.history.replaceState({ascInsights: true}, '', window.location.href);
  initInsightsBoard(board);
  window.addEventListener('popstate', onPopState);
}
