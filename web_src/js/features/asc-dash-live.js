import {GET} from '../modules/fetch.js';
import {initAscFeedFilter} from './asc-feed-filter.js';

export const REFRESH_MS = 60_000;
const MAX_RETRY_S = 60;

/** Seconds to the next try after `failures` failed loads in a row: 8, 16, 32, then 60. */
export function retryDelay(failures) {
  return Math.min(8 * 2 ** Math.max(failures - 1, 0), MAX_RETRY_S);
}

/**
 * Puts the numbers of the work switcher (GET /-/asc/dash/counts) on every [data-asc-live-count] of the page. A failed
 * load keeps the numbers shown: the Today card states the offline state.
 */
export async function refreshCounts(doc, load) {
  const url = doc.querySelector('[data-asc-dash-counts]')?.getAttribute('data-asc-dash-counts');
  if (!url) return;
  try {
    const resp = await load(url, {cache: 'no-store'});
    if (!resp.ok) return;
    const counts = await resp.json();
    for (const el of doc.querySelectorAll('[data-asc-live-count]')) {
      const n = counts[el.getAttribute('data-asc-live-count')];
      if (Number.isInteger(n)) el.textContent = String(n);
    }
  } catch {
    // keep the numbers shown
  }
}

/**
 * The "Today" card of the dashboard (templates/user/dashboard/asc_today.tmpl, board Dashboard): it loads itself again
 * every minute while the page is in view and nobody uses the card (focus in it, or a filter other than All). When a
 * load fails it keeps its items and shows the offline state, with Try again and the seconds to the next try; each
 * next try waits longer, and the connection coming back tries at once. Each load also refreshes the counts of the work
 * switcher.
 */
export function initAscDashLive({doc = document, win = window, load = GET, refreshMs = REFRESH_MS} = {}) {
  let card = doc.querySelector('[data-asc-dash-today]');
  if (!card) return;
  const url = card.getAttribute('data-asc-dash-today');
  let failures = 0;
  let timer = 0;
  let ticker = 0;
  let busy = false;
  let due = false;

  const offlineBox = () => card.querySelector('[data-asc-dash-offline]');
  const inUse = () => card.contains(doc.activeElement) ||
    Boolean(card.querySelector('[data-feed-filter] button[aria-pressed="true"]:not([data-kind=""])'));
  const stop = () => {
    win.clearTimeout(timer);
    win.clearInterval(ticker);
  };

  function later(ms) {
    stop();
    timer = win.setTimeout(() => {
      if (doc.hidden) {
        due = true;
      } else if (inUse()) {
        later(refreshMs);
      } else {
        refresh(false);
      }
    }, ms);
  }
  const showOffline = (seconds) => {
    const box = offlineBox();
    if (!box) return;
    const count = card.querySelector('[data-asc-dash-countdown]');
    box.hidden = false;
    let left = seconds;
    const show = () => {
      if (count) count.textContent = (count.getAttribute('data-template') ?? '').replace('%d', String(left));
    };
    show();
    ticker = win.setInterval(() => {
      left--;
      if (left > 0) {
        show();
      } else if (doc.hidden) {
        win.clearInterval(ticker);
        due = true;
      } else {
        refresh(false);
      }
    }, 1000);
  };

  async function refresh(byHand) {
    if (busy) return;
    busy = true;
    due = false;
    stop();
    try {
      const resp = await load(url);
      const html = resp.ok ? await resp.text() : '';
      const fresh = new DOMParser().parseFromString(html, 'text/html').querySelector('[data-asc-dash-today]');
      if (!fresh) throw new Error(`the Today card did not load (${resp.status})`);
      const node = doc.importNode(fresh, true);
      card.replaceWith(node);
      card = node;
      initAscFeedFilter(card);
      failures = 0;
      await refreshCounts(doc, load);
      if (byHand) {
        // Try again is gone with the old card: the focus goes to the heading of the new one
        const heading = card.querySelector('h2');
        heading?.setAttribute('tabindex', '-1');
        heading?.focus();
      }
      later(refreshMs);
    } catch {
      failures++;
      showOffline(retryDelay(failures));
    } finally {
      busy = false;
    }
  }

  doc.addEventListener('click', (e) => {
    if (e.target instanceof Element && e.target.closest('[data-asc-dash-retry]') && card.contains(e.target)) refresh(true);
  });
  doc.addEventListener('visibilitychange', () => {
    if (!doc.hidden && due) refresh(false);
  });
  win.addEventListener('online', () => {
    if (offlineBox()?.hidden === false) refresh(false);
  });
  later(refreshMs);
}
