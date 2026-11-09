// New items of the inbox appear while it is open (board Inbox: "new items slide in from the top without jumping the
// list"). Every minute while the page is in view the script loads the rows of the same tab and filters again
// (GET /-/asc/inbox/rows, templates/user/asc_inbox_live.tmpl) and adds the rows the list does not have, each where
// the server puts it (with its day heading when that is new too). Rows are never taken away or moved: what the person
// reads stays where it is, and when they scrolled down the list is scrolled by the height of what came in above. A
// polite live region says how many items came in. An empty inbox shows a button that loads the page with them.
// Without this script the page is the same, a reload shows the new items.

import {GET} from '../modules/fetch.js';

export const REFRESH_MS = 60_000;
const NEW_MS = 6_000;

const keyOf = (el) => el.getAttribute('data-asc-key') ?? '';
const isItem = (key) => key.startsWith('pull:') || key.startsWith('run:');

/** The text of a count: one for 1, many with %d for the others. */
export function countLabel(count, one, many) {
  return count === 1 ? one : many.replace('%d', String(count));
}

/** The nearest box of el that scrolls, else null for the window. */
function scrollBox(el, win) {
  for (let p = el.parentElement; p; p = p.parentElement) {
    const o = win.getComputedStyle(p).overflowY;
    if ((o === 'auto' || o === 'scroll') && p.scrollHeight > p.clientHeight) return p;
  }
  return null;
}

/**
 * Adds to list the children of fresh (the rows the server lists, in its order) whose data-asc-key the list does not
 * have, each before the next row of fresh the list has, else at the end. Returns the added rows.
 */
export function mergeRows(list, fresh) {
  const have = new Map();
  for (const el of list.children) {
    const key = keyOf(el);
    if (key) have.set(key, el);
  }
  const items = Array.from(fresh.children);
  const added = [];
  for (const [i, el] of items.entries()) {
    const key = keyOf(el);
    if (!key || have.has(key)) continue;
    const next = items.slice(i + 1).map(keyOf).find((k) => have.has(k));
    const node = list.ownerDocument.importNode(el, true);
    list.insertBefore(node, next ? have.get(next) : null);
    added.push(node);
  }
  return added;
}

/**
 * Loads the rows again and adds the new ones to list. Returns how many items came in (day headings are not counted).
 */
export async function refreshInbox(list, {doc = document, win = window, load = GET} = {}) {
  if (list.querySelector('.is-swiping')) return 0;
  const resp = await load(list.getAttribute('data-asc-inbox-live'), {cache: 'no-store'});
  if (!resp.ok) return 0;
  const fresh = new DOMParser().parseFromString(await resp.text(), 'text/html').querySelector('[data-asc-inbox-rows]');
  if (!fresh) return 0;
  if (list.tagName !== 'UL') {
    // the empty inbox: a button that loads the page with the items
    const n = Array.from(fresh.children).filter((el) => isItem(keyOf(el))).length;
    const more = list.querySelector('[data-asc-inbox-more]');
    if (n > 0 && more) {
      more.textContent = countLabel(n, more.getAttribute('data-one') ?? '', more.getAttribute('data-n') ?? '');
      more.setAttribute('href', win.location.href);
      more.hidden = false;
    }
    return n;
  }
  // the first row in view keeps its place unless the person is at the top of the list
  const rows = Array.from(list.children);
  const anchor = rows.find((el) => el.getBoundingClientRect().bottom > 0);
  const scrolled = anchor && anchor !== rows[0];
  const before = scrolled ? anchor.getBoundingClientRect().top : 0;
  const added = mergeRows(list, fresh);
  if (added.length === 0) return 0;
  if (scrolled) {
    const delta = anchor.getBoundingClientRect().top - before;
    const box = scrollBox(list, win);
    if (delta !== 0) {
      if (box) box.scrollTop += delta;
      else win.scrollBy(0, delta);
    }
  }
  for (const node of added) {
    node.classList.add('is-new');
    win.setTimeout(() => node.classList.remove('is-new'), NEW_MS);
  }
  const n = added.filter((el) => isItem(keyOf(el))).length;
  const news = doc.querySelector('[data-asc-inbox-news]');
  if (news && n > 0) news.textContent = countLabel(n, news.getAttribute('data-one') ?? '', news.getAttribute('data-n') ?? '');
  return n;
}

export function initAscInboxLive({doc = document, win = window, load = GET, refreshMs = REFRESH_MS} = {}) {
  const list = doc.querySelector('[data-asc-inbox-live]');
  if (!list) return;
  let due = false;
  let busy = false;
  const later = () => win.setTimeout(tick, refreshMs);
  async function tick() {
    if (doc.hidden) {
      due = true;
      return;
    }
    if (busy) return;
    busy = true;
    try {
      await refreshInbox(list, {doc, win, load});
    } catch {
      // keep the list as it is; the next minute tries again
    } finally {
      busy = false;
    }
    later();
  }
  doc.addEventListener('visibilitychange', () => {
    if (!doc.hidden && due) {
      due = false;
      tick();
    }
  });
  later();
}
