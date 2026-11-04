// The operation log (templates/repo/asc_ops.tmpl): selecting a row that can be undone, its Undo, or Undo restack in the
// banner loads the dry run into the panel beside the list instead of leaving the page; on a phone it opens as a bottom
// sheet over the list (board Mobile-Ops). Without this script the same links open the confirmation page.
import {GET} from '../modules/fetch.js';

/** The part of an undo confirmation page that goes into the panel, taken from the parsed page and never from a string of markup. */
export function undoFragment(doc) {
  const root = doc.querySelector('[data-testid="asc-undo"]');
  if (!root) return null;
  const nodes = Array.from(root.children).filter((el) => el.tagName !== 'HEADER');
  return nodes.length ? nodes : null;
}

/**
 * The Undo link a click selects: the link itself, the Undo of the row that was clicked, or Undo restack in the banner.
 * null for any other link or control, for a row that cannot be undone, and while text is selected (the hashes are
 * selectable text).
 */
export function undoLinkOf(target, selection = '') {
  const link = target.closest('a[data-testid="asc-op-undo"], a[data-asc-ops-load]');
  if (link) return link;
  if (selection || target.closest('a, button, input, label, summary')) return null;
  return target.closest('.rv-op')?.querySelector('a[data-testid="asc-op-undo"]') ?? null;
}

export function initAscOps() {
  const list = document.querySelector('[data-testid="asc-ops-list"]');
  const panel = document.querySelector('[data-asc-ops-panel]');
  if (!list || !panel) return;
  const body = panel.querySelector('[data-asc-ops-panel-body]');
  const original = Array.from(body.childNodes);
  const sheet = document.querySelector('[data-asc-ops-sheet]');
  const sheetBody = sheet?.querySelector('[data-asc-ops-sheet-body]');
  const phone = window.matchMedia('(max-width: 639.98px)');

  const select = (href) => {
    for (const row of list.querySelectorAll('.rv-op')) {
      row.classList.toggle('is-selected', row.querySelector('a[data-testid="asc-op-undo"]')?.href === href);
    }
  };
  const show = (nodes) => {
    body.replaceChildren(...nodes);
    body.classList.remove('is-swapped');
    requestAnimationFrame(() => body.classList.add('is-swapped')); // restarts the crossfade when another row is selected
  };
  const restore = () => {
    if (sheet?.open) sheet.close(); // the close event clears the selection
    show(original);
    select('');
  };
  sheet?.addEventListener('close', () => {
    sheetBody.replaceChildren();
    select('');
  });
  phone.addEventListener('change', () => {
    if (!phone.matches && sheet?.open) sheet.close(); // turned to a wider screen: the side panel takes over
  });
  sheet?.addEventListener('click', (e) => {
    if (e.target === sheet) sheet.close(); // a tap on the backdrop
  });

  const load = async (link) => {
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
    select(link.href);
    const imported = nodes.map((n) => document.importNode(n, true));
    const inSheet = Boolean(sheet && phone.matches);
    const target = inSheet ? sheetBody : body;
    if (inSheet) sheetBody.replaceChildren(...imported);
    else show(imported);
    let cancel = null;
    for (const a of target.querySelectorAll('a.rv-btn-secondary[href]:not([data-testid])')) {
      if (!a.textContent.trim()) continue;
      cancel = a;
      a.addEventListener('click', (ev) => {
        ev.preventDefault();
        restore();
      });
    }
    if (inSheet) {
      if (!sheet.open) sheet.showModal();
      cancel?.focus(); // not the reason field: the keyboard would cover the sheet
      return;
    }
    panel.scrollIntoView({block: 'nearest'});
    body.querySelector('input[name="reason"]')?.focus();
  };

  document.addEventListener('click', (e) => {
    if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    const inList = list.contains(e.target);
    if (!inList && !e.target.closest('a[data-asc-ops-load]')) return;
    const link = undoLinkOf(e.target, inList ? String(window.getSelection?.() ?? '') : '');
    if (!link) return;
    e.preventDefault();
    load(link);
  });
}
