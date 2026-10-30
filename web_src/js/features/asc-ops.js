// The operation log (templates/repo/asc_ops.tmpl): choosing Undo on a row loads the dry run into the panel beside the list instead
// of leaving the page. Without this script the same link opens the confirmation page.
import {GET} from '../modules/fetch.js';

/** The part of an undo confirmation page that goes into the panel, taken from the parsed page and never from a string of markup. */
export function undoFragment(doc) {
  const root = doc.querySelector('[data-testid="asc-undo"]');
  if (!root) return null;
  const nodes = Array.from(root.children).filter((el) => el.tagName !== 'HEADER');
  return nodes.length ? nodes : null;
}

export function initAscOps() {
  const list = document.querySelector('[data-testid="asc-ops-list"]');
  const panel = document.querySelector('[data-asc-ops-panel]');
  if (!list || !panel) return;
  const body = panel.querySelector('[data-asc-ops-panel-body]');
  const original = Array.from(body.childNodes);

  const restore = () => {
    body.replaceChildren(...original);
    for (const row of list.querySelectorAll('.rv-op.is-selected')) row.classList.remove('is-selected');
  };

  list.addEventListener('click', async (e) => {
    const link = e.target.closest('a[data-testid="asc-op-undo"]');
    if (!link || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    e.preventDefault();
    let res;
    try {
      res = await GET(link.href, {headers: {Accept: 'text/html'}});
    } catch {
      window.location.assign(link.href);
      return;
    }
    const nodes = res.ok ? undoFragment(new DOMParser().parseFromString(await res.text(), 'text/html')) : null;
    if (!nodes) {
      window.location.assign(link.href); // a refusal or a changed log: the page says it
      return;
    }
    for (const row of list.querySelectorAll('.rv-op.is-selected')) row.classList.remove('is-selected');
    link.closest('.rv-op')?.classList.add('is-selected');
    body.replaceChildren(...nodes.map((n) => document.importNode(n, true)));
    for (const cancel of body.querySelectorAll('a[href]:not([data-testid])')) {
      if (cancel.textContent.trim() && cancel.classList.contains('rv-btn-secondary')) {
        cancel.addEventListener('click', (ev) => {
          ev.preventDefault();
          restore();
        });
      }
    }
    panel.scrollIntoView({block: 'nearest'});
    body.querySelector('input[name="reason"]')?.focus();
  });
}
