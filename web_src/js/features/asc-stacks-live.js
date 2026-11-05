// The Stacks list (board Stacks) follows its repository while it is open. The hub's event stream says when a pull
// request of a repository the viewer can read changed (event asc-stacks: a JSON list of repository IDs); the list then
// loads its rows again. A row whose status changed is swapped and its chip pulses once; a stack that came or went
// replaces the rows whole, without a pulse. The pressed filter and the search apply again to the new rows.

import {GET} from '../modules/fetch.js';

function rowKeys(rows) {
  return rows.map((r) => r.getAttribute('data-stack-key')).toSorted().join('\n');
}

// The row as the server drew it, without what the filters, the search and a pulse added in the page.
function served(row) {
  const copy = row.cloneNode(true);
  for (const a of ['hidden', 'data-filter-miss', 'data-search-miss']) copy.removeAttribute(a);
  copy.querySelector('[data-stack-chip]')?.removeAttribute('data-stack-pulse');
  return copy.outerHTML;
}

function reapplyFilters() {
  document.querySelector('[data-stack-filter] button[aria-pressed="true"]')?.click();
  document.querySelector('input[data-stack-search]')?.dispatchEvent(new Event('input'));
}

function pulse(row) {
  const chip = row.querySelector('[data-stack-chip]');
  if (!chip) return;
  chip.setAttribute('data-stack-pulse', '');
  chip.addEventListener('animationend', () => chip.removeAttribute('data-stack-pulse'), {once: true});
}

// Loads url and brings the rows of list up to date. Returns the number of rows whose status changed.
export async function reloadStacks(list, url) {
  const res = await GET(url);
  if (!res.ok) return 0;
  const doc = new DOMParser().parseFromString(await res.text(), 'text/html');
  const next = doc.querySelector(`[data-stack-live="${list.getAttribute('data-stack-live')}"]`);
  if (!next) return 0;
  const oldRows = Array.from(list.querySelectorAll('[data-stack-row]'));
  const newRows = Array.from(next.querySelectorAll('[data-stack-row]'));
  if (rowKeys(oldRows) !== rowKeys(newRows)) {
    list.replaceChildren(...newRows.map((r) => document.importNode(r, true)));
    reapplyFilters();
    return 0;
  }
  const byKey = new Map(oldRows.map((r) => [r.getAttribute('data-stack-key'), r]));
  let changed = 0;
  let swapped = 0;
  for (const fresh of newRows) {
    const old = byKey.get(fresh.getAttribute('data-stack-key'));
    if (served(old) === served(fresh)) continue;
    const row = document.importNode(fresh, true);
    old.replaceWith(row);
    swapped++;
    if (old.getAttribute('data-status') !== row.getAttribute('data-status')) {
      pulse(row);
      changed++;
    }
  }
  if (swapped) reapplyFilters();
  return changed;
}

// Runs reload at most once at a time; an event that comes during a load asks for one more.
export function follower(reload) {
  let busy = false;
  let again = false;
  const run = async () => {
    if (busy) {
      again = true;
      return;
    }
    busy = true;
    try {
      do {
        again = false;
        await reload();
      } while (again);
    } finally {
      busy = false;
    }
  };
  return run;
}

export function mentionsRepo(data, repoID) {
  try {
    const ids = JSON.parse(data);
    return Array.isArray(ids) && ids.includes(repoID);
  } catch {
    return false;
  }
}

export function initAscStacksLive() {
  const list = document.querySelector('[data-stack-live]');
  const {appSubUrl, notificationSettings, assetVersionEncoded} = window.config;
  if (!list || !window.SharedWorker || !window.EventSource || (notificationSettings?.EventSourceUpdateTime ?? 0) <= 0) return;
  const repoID = Number(list.getAttribute('data-stack-live'));
  const run = follower(() => reloadStacks(list, window.location.href).catch(() => 0));
  const worker = new SharedWorker(`${__webpack_public_path__}js/eventsource.sharedworker.js?v=${assetVersionEncoded}`, 'notification-worker');
  worker.port.addEventListener('message', (e) => {
    if (e.data?.type === 'asc-stacks' && mentionsRepo(e.data.data, repoID)) run();
  });
  worker.port.start();
  worker.port.postMessage({type: 'start', url: `${window.location.origin}${appSubUrl}/user/events`});
  worker.port.postMessage({type: 'listen', eventType: 'asc-stacks'});
}
