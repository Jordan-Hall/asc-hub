const skeletonRows = 5;

/** Puts skeleton rows in place of the rows of an issue list, and says to screen readers that it is loading. */
export function showListLoading(list) {
  if (list.classList.contains('is-loading')) return;
  list.classList.add('is-loading');
  list.setAttribute('aria-busy', 'true');
  for (let i = 0; i < skeletonRows; i++) {
    const row = document.createElement('li');
    row.className = 'ds-li wk-issue-sk';
    row.setAttribute('aria-hidden', 'true');
    for (const part of ['wk-issue-sk-title', 'wk-issue-sk-meta']) {
      const bar = document.createElement('span');
      bar.className = `ds-sk ${part}`;
      row.append(bar);
    }
    list.append(row);
  }
  const status = document.createElement('li');
  status.className = 'ds-sr wk-issue-sk';
  status.setAttribute('role', 'status');
  status.textContent = list.getAttribute('data-loading') ?? '';
  list.append(status);
}

/** Gives the list its rows back, for a page restored from the back-forward cache. */
export function hideListLoading(list) {
  list.classList.remove('is-loading');
  list.removeAttribute('aria-busy');
  for (const row of list.querySelectorAll('.wk-issue-sk')) row.remove();
}

/** Whether following the link loads the same list with other filters: same page, other query, plain click. */
export function reloadsList(link, e, here = window.location) {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return false;
  if (link.target && link.target !== '_self') return false;
  if (link.hasAttribute('download')) return false;
  const to = new URL(link.href, here.href);
  return to.origin === here.origin && to.pathname === here.pathname && to.search !== here.search;
}

/**
 * The issue and pull request lists (templates/shared/issuelist.tmpl): a filter, a state chip, a label of the rail,
 * "Clear filters" or the search loads the page again, and until it does the list shows skeleton rows.
 */
export function initIssueListLoading(doc = document) {
  const list = doc.querySelector('#issue-list');
  if (!list) return;
  doc.addEventListener('click', (e) => {
    const link = e.target.closest?.('a[href]');
    if (link && reloadsList(link, e)) showListLoading(list);
  });
  doc.addEventListener('submit', (e) => {
    if (!e.defaultPrevented && e.target.matches('.issue-list-search')) showListLoading(list);
  });
  window.addEventListener('pageshow', (e) => {
    if (e.persisted) hideListLoading(list);
  });
}
