// The Filters bottom sheet of the search page on a phone (board Search, mobile). Below 900 px the facets move
// into dialog[data-search-filters] and the Filters button opens it; at 900 px and wider they go back to the
// sidebar. Without scripts the facets stay in the page as rows of chips.

export const narrowFilters = '(max-width: 899.98px)';

export function initAscSearchFilters(root = document) {
  const facets = root.querySelector('.asc-search-facets');
  const dialog = root.querySelector('dialog[data-search-filters]');
  const body = dialog?.querySelector('[data-search-filters-body]');
  const open = root.querySelector('[data-search-filters-open]');
  if (!facets || !body || !open || typeof dialog.showModal !== 'function') return;
  const home = document.createComment('asc-search-facets');
  facets.before(home);
  const place = (narrow) => {
    open.hidden = !narrow;
    if (narrow) {
      body.append(facets);
    } else {
      if (dialog.open) dialog.close();
      home.after(facets);
    }
  };
  const media = window.matchMedia(narrowFilters);
  place(media.matches);
  media.addEventListener?.('change', (e) => place(e.matches));
  open.addEventListener('click', () => dialog.showModal());
  dialog.querySelector('[data-search-filters-close]')?.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => open.focus());
}
