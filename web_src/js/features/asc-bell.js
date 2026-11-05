import {GET, POST} from '../modules/fetch.js';
import {showErrorToast} from '../modules/toast.js';

/**
 * Puts the inside of the popover (GET /-/asc/bell, user/notification/asc_bell.tmpl) into it. A failed load shows the
 * failed line with a link to the notifications page. Returns whether it loaded.
 */
export async function fill(pop, load) {
  const body = pop.querySelector('[data-asc-bell-body]');
  if (!body) return false;
  body.setAttribute('aria-busy', 'true');
  let fresh = null;
  try {
    const resp = await load(pop.getAttribute('data-asc-bell'), {cache: 'no-store'});
    const html = resp.ok ? await resp.text() : '';
    fresh = new DOMParser().parseFromString(html, 'text/html').querySelector('[data-asc-bell-content]');
  } catch {
    fresh = null;
  }
  body.setAttribute('aria-busy', 'false');
  if (!fresh) {
    body.querySelector('[data-asc-bell-loading]')?.classList.add('tw-hidden');
    body.querySelector('[data-asc-bell-failed]')?.classList.remove('tw-hidden');
    return false;
  }
  const old = body.querySelector('[data-asc-bell-content]');
  const node = pop.ownerDocument.importNode(fresh, true);
  if (old) {
    old.replaceWith(node);
  } else {
    body.replaceChildren(node);
  }
  return true;
}

/** Shows everything of the popover and the header bell as read. */
export function markShownRead(doc, pop) {
  for (const it of pop.querySelectorAll('.ds-bell-it.is-unread')) {
    it.classList.remove('is-unread');
    for (const mark of it.querySelectorAll('.ds-dot, .tw-sr-only')) mark.remove();
  }
  pop.querySelector('[data-asc-bell-count]')?.remove();
  pop.querySelector('[data-asc-bell-read]')?.remove();
  for (const pip of doc.querySelectorAll('.notification_count')) {
    pip.textContent = '0';
    pip.classList.add('tw-hidden');
  }
}

async function readAll(doc, pop, button, send) {
  button.disabled = true;
  try {
    const resp = await send(button.getAttribute('data-asc-bell-read'));
    if (!resp.ok) throw new Error(`mark all read answered ${resp.status}`);
    markShownRead(doc, pop);
    pop.querySelector('.ds-bell-it')?.focus();
  } catch {
    button.disabled = false;
    showErrorToast(button.getAttribute('data-failed'));
  }
}

/**
 * The notifications popover of the header bell (templates/base/head_navbar.tmpl #ds-bell, board Shell 03). ds-shell.js
 * opens and closes it; each time it opens its inside loads again, and the focus goes to its first link when it was
 * on the bell. Mark all read posts and shows everything read without a reload.
 */
export function initAscBell({doc = document, load = GET, send = POST} = {}) {
  const pop = doc.querySelector('[data-asc-bell]');
  if (!pop) return;
  pop.addEventListener('ds-open', async () => {
    const opener = doc.activeElement;
    const ok = await fill(pop, load);
    if (ok && pop.classList.contains('is-open') && (pop.contains(opener) || opener === doc.activeElement)) {
      pop.querySelector('[data-asc-bell-read], .ds-bell-it, .ds-bell-foot a')?.focus();
    }
  });
  pop.addEventListener('click', (e) => {
    const button = e.target instanceof Element ? e.target.closest('[data-asc-bell-read]') : null;
    if (!button) return;
    e.preventDefault();
    readAll(doc, pop, button, send);
  });
}
