// The list or file while the next page loads (boards Code and History): a plain click on a link inside
// [data-loading-links], or a submit of form[data-loading-form], puts the skeleton [data-loading-skel] in the place of
// [data-loading-body], so the page answers at once. The page coming back from history shows the old content again.

/** Whether a click leaves for another page of this site in this tab. */
export function leavesForPage(e, link, loc = window.location) {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return false;
  if (link.target && link.target !== '_self') return false;
  if (link.hasAttribute('download') || link.getAttribute('aria-current')) return false;
  const url = new URL(link.href, loc.href);
  return url.origin === loc.origin && (url.pathname !== loc.pathname || url.search !== loc.search);
}

export function setPageLoading(on, root = document) {
  const body = root.querySelector('[data-loading-body]');
  const skeleton = root.querySelector('[data-loading-skel]');
  if (!body || !skeleton) return false;
  body.hidden = on;
  skeleton.hidden = !on;
  return true;
}

export function initAscPageLoading(root = document) {
  if (!root.querySelector('[data-loading-skel]')) return;
  root.addEventListener('click', (e) => {
    const link = e.target.closest?.('[data-loading-links] a[href]');
    if (link && leavesForPage(e, link)) setPageLoading(true, root);
  });
  root.addEventListener('submit', (e) => {
    if (!e.defaultPrevented && e.target.matches?.('form[data-loading-form]')) setPageLoading(true, root);
  });
  window.addEventListener('pageshow', () => setPageLoading(false, root));
}
