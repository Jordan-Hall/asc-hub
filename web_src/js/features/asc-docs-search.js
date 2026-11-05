import {GET} from '../modules/fetch.js';

// The documentation search (board Docs): Ctrl K or "/" moves the focus to the search box of a docs page, and on the
// start page the results follow the typing: the results alone load from /-/docs?q=&fragment=results a moment after the
// last key, with a loading state, and the address keeps the query. A failed load keeps the results already shown.

export const DELAY = 250;

function isTyping(el) {
  const tag = el?.tagName?.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || el?.isContentEditable === true;
}

/** Whether a key press should open the docs search: Ctrl K or Cmd K anywhere, "/" outside a field. */
export function isSearchKey(e) {
  if (e.altKey || e.shiftKey) return false;
  if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') return true;
  return e.key === '/' && !e.ctrlKey && !e.metaKey && !isTyping(e.target);
}

/** The address of the results alone for a query. */
export function resultsURL(action, q) {
  const u = new URL(action, window.location.href);
  u.search = new URLSearchParams({q, fragment: 'results'}).toString();
  return `${u.pathname}${u.search}`;
}

let keyHandler = null;

function liveSearch(input, live, load) {
  const loading = live.querySelector('[data-asc-docs-loading]');
  const status = live.querySelector('[data-asc-docs-status]');
  let timer = 0;
  let seq = 0;
  const done = () => {
    loading.hidden = true;
    live.removeAttribute('aria-busy');
  };
  const run = async (q, mine) => {
    try {
      const resp = await load(resultsURL(input.form.getAttribute('action'), q));
      if (!resp.ok) throw new Error(`docs search answered ${resp.status}`);
      const doc = new DOMParser().parseFromString(await resp.text(), 'text/html');
      const fresh = doc.querySelector('[data-asc-docs-results]');
      if (mine !== seq || !fresh) return;
      live.querySelector('[data-asc-docs-results]')?.remove();
      live.append(document.importNode(fresh, true));
      if (status) status.textContent = (status.getAttribute('data-template') ?? '').replace('%d', String(fresh.querySelectorAll('.site-hit').length));
      const u = new URL(window.location.href);
      u.searchParams.set('q', q);
      window.history.replaceState(window.history.state, '', u);
    } catch {
      // the results already shown stay; Enter still sends the form
    } finally {
      if (mine === seq) done();
    }
  };
  input.addEventListener('input', () => {
    clearTimeout(timer);
    const q = input.value.trim();
    seq++;
    const mine = seq;
    if (!q) {
      live.querySelector('[data-asc-docs-results]')?.remove();
      if (status) status.textContent = '';
      done();
      return;
    }
    timer = setTimeout(() => {
      loading.hidden = false;
      live.setAttribute('aria-busy', 'true');
      run(q, mine);
    }, DELAY);
  });
}

export function initAscDocsSearch({doc = document, load = GET} = {}) {
  const input = doc.querySelector('input[data-asc-docs-q]');
  if (keyHandler) window.removeEventListener('keydown', keyHandler, true);
  keyHandler = null;
  if (!input) return;
  // before the shell's palette, which listens on the document
  keyHandler = (e) => {
    if (!isSearchKey(e) || doc.activeElement === input) return;
    e.preventDefault();
    e.stopImmediatePropagation();
    input.focus();
    input.select();
  };
  window.addEventListener('keydown', keyHandler, true);
  const live = doc.querySelector('[data-asc-docs-live]');
  if (live && input.form) liveSearch(input, live, load);
}
