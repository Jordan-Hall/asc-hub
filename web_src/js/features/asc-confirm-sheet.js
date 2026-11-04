// The stack operations of a pull request on a phone (board Mobile-ReviewVersions): Merge, Merge when ready, Restack,
// Revert and Remove from the queue open their confirmation (templates/repo/asc_confirm.tmpl) as a bottom sheet over the
// page, with the focus on Cancel, instead of leaving the page. The sheet holds the parts of the confirmation page
// itself, so the dry run, the scope and the button are the same; a wider screen and a page without this script open
// the page. After a merge the result with its Undo also shows as a toast, above the tab bar on a phone.
import {GET} from '../modules/fetch.js';
import {showDsToast} from './ds-toast.js';

export const PHONE = '(max-width: 639.98px)';

/** The title and the parts of a confirmation page that go into the sheet, from the parsed page; null when it is not one. */
export function confirmFragment(doc) {
  const root = doc.querySelector('[data-testid="asc-confirm"]');
  const title = root?.querySelector('.rv-title')?.textContent.trim();
  if (!title) return null;
  const nodes = Array.from(root.children).filter((el) => el.tagName !== 'HEADER');
  return nodes.length ? {title, nodes} : null;
}

/** The URL a GET form of the sheet (the scope of a land, the options of a revert) asks for. */
export function formURL(form, base) {
  const url = new URL(form.getAttribute('action'), base);
  url.search = new URLSearchParams(new FormData(form)).toString();
  return url.href;
}

export function initAscConfirmSheet(doc = document, win = window) {
  const sheet = doc.querySelector('[data-asc-confirm-sheet]');
  if (!sheet) return;
  const heading = sheet.querySelector('.rv-sheet-h');
  const body = sheet.querySelector('[data-asc-confirm-sheet-body]');
  const phone = win.matchMedia(PHONE);

  const load = async (href) => {
    let part = null;
    try {
      const res = await GET(href, {headers: {Accept: 'text/html'}});
      if (res.ok) part = confirmFragment(new DOMParser().parseFromString(await res.text(), 'text/html'));
    } catch {
      part = null;
    }
    if (!part) {
      win.location.assign(href); // a refusal or an error: the page says it
      return;
    }
    heading.textContent = part.title;
    body.replaceChildren(...part.nodes.map((n) => doc.importNode(n, true)));
    for (const el of body.querySelectorAll('[autofocus]')) el.removeAttribute('autofocus');
    if (!sheet.open) sheet.showModal();
    const cancel = body.querySelector('[data-testid="asc-confirm-cancel"]') ?? body.querySelector('.asc-op-buttons a.rv-btn-secondary');
    cancel?.focus(); // every operation here writes: the safe choice comes first
  };

  sheet.addEventListener('close', () => {
    body.replaceChildren();
    heading.textContent = '';
  });
  sheet.addEventListener('click', (e) => {
    if (e.target === sheet) {
      sheet.close(); // a tap on the backdrop
      return;
    }
    const cancel = e.target.closest('.asc-op-buttons a.rv-btn-secondary');
    if (cancel) {
      e.preventDefault();
      sheet.close();
    }
  });
  sheet.addEventListener('submit', (e) => {
    const form = e.target;
    if (form.method !== 'get') return; // the operation itself posts and leaves the page
    e.preventDefault();
    load(formURL(form, win.location.href));
  });
  phone.addEventListener('change', () => {
    if (!phone.matches && sheet.open) sheet.close();
  });

  doc.addEventListener('click', (e) => {
    if (!phone.matches || e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
    const link = e.target.closest('a[data-asc-confirm-open]');
    if (!link) return;
    e.preventDefault();
    load(link.href);
  });
}

/**
 * The text and the Undo link of a success message that offers an undo (a merge, ascMergedMessage), or null. The text
 * is the message without the link.
 */
export function undoToastOf(flash) {
  const link = flash?.querySelector('a[href*="/asc/ops/undo?"]');
  if (!link) return null;
  const copy = flash.cloneNode(true);
  copy.querySelector('a[href*="/asc/ops/undo?"]').remove();
  return {text: copy.textContent.replace(/\s+/g, ' ').trim(), label: link.textContent.trim(), href: link.href};
}

/**
 * After a merge the result and its Undo also show as a toast where the eye is (board ReviewVersions, toast). A toast that
 * holds an Undo stays until it is dismissed.
 */
export function initAscUndoToast(doc = document, win = window) {
  const toast = undoToastOf(doc.querySelector('#flash-message.flash-success'));
  if (!toast) return;
  showDsToast(toast.text, {actionLabel: toast.label, onAction: () => win.location.assign(toast.href), duration: 0});
}
