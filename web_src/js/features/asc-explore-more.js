import {GET} from '../modules/fetch.js';

export const PHONE = '(max-width: 767.98px)';

/**
 * More rows of a paged list on a phone (templates/shared/ds/more.tmpl, board Mobile-Explore): the pager gives way to
 * the next pages, which load as the end of the list comes near. Load more loads them where that cannot happen: no
 * IntersectionObserver, data saver on, or a load that failed. A Load more press moves the focus to the first new row.
 */
export function initAscExploreMore({doc = document, win = window, load = GET} = {}) {
  const box = doc.querySelector('[data-asc-more]');
  if (!box || !win.matchMedia?.(PHONE).matches) return;
  const selector = box.getAttribute('data-asc-more');
  const list = doc.querySelector(selector);
  if (!list) return;
  const button = box.querySelector('[data-asc-more-btn]');
  const status = box.querySelector('[data-asc-more-status]');
  let next = box.getAttribute('data-asc-more-next');
  let busy = false;
  let observer = null;

  box.hidden = false;
  for (const pager of box.parentElement.querySelectorAll('.ds-pager-wrap')) pager.hidden = true;

  const say = (text) => {
    if (status) status.textContent = text;
  };
  const stopWatching = () => {
    observer?.disconnect();
    observer = null;
  };

  async function more(byHand) {
    if (busy || !next) return;
    busy = true;
    box.setAttribute('aria-busy', 'true');
    try {
      const resp = await load(next);
      const html = resp.ok ? await resp.text() : '';
      const page = new DOMParser().parseFromString(html, 'text/html');
      const rows = page.querySelector(selector);
      if (!rows) throw new Error(`the next page did not load (${resp.status})`);
      const added = Array.from(rows.children, (row) => doc.importNode(row, true));
      list.append(...added);
      say((status?.getAttribute('data-template') ?? '').replace('%d', String(added.length)));
      next = page.querySelector('[data-asc-more]')?.getAttribute('data-asc-more-next') ?? '';
      if (byHand) added[0]?.querySelector('a[href]')?.focus();
      if (next) {
        // a list still short of the screen's end: look again, as no new crossing will come
        observer?.unobserve(box);
        observer?.observe(box);
      } else {
        stopWatching();
        button.hidden = true;
      }
    } catch {
      // the button stays for the reader to try again
      stopWatching();
      say(status?.getAttribute('data-failed') ?? '');
    } finally {
      busy = false;
      box.removeAttribute('aria-busy');
    }
  }

  button.addEventListener('click', () => more(true));
  if ('IntersectionObserver' in win && !win.navigator?.connection?.saveData) {
    observer = new win.IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) more(false);
    }, {rootMargin: '0px 0px 400px 0px'});
    observer.observe(box);
  }
}
