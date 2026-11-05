import {initAscSearchFilters} from './asc-search-filters.js';

function page(narrow) {
  const listeners = [];
  window.matchMedia = vi.fn(() => ({matches: narrow, addEventListener: (_, fn) => listeners.push(fn)}));
  document.body.innerHTML = `<div class="asc-search-layout">
    <div class="asc-search-facets"><a class="asc-chip" href="?lang=Rust">Rust</a></div>
    <button type="button" data-search-filters-open hidden>Filters</button>
    <dialog data-search-filters><div data-search-filters-body></div><button type="button" data-search-filters-close>Show</button></dialog>
  </div>`;
  const dialog = document.querySelector('dialog');
  dialog.showModal = vi.fn(() => dialog.setAttribute('open', ''));
  dialog.close = vi.fn(() => {
    dialog.removeAttribute('open');
    dialog.dispatchEvent(new Event('close'));
  });
  initAscSearchFilters(document);
  return {dialog, change: (matches) => {
    for (const fn of listeners) fn({matches});
  }};
}

test('a phone moves the facets into the sheet and opens it from the button', () => {
  const {dialog} = page(true);
  const open = document.querySelector('[data-search-filters-open]');
  expect(open.hidden).toBe(false);
  expect(dialog.querySelector('.asc-search-facets')).not.toBeNull();
  open.click();
  expect(dialog.showModal).toHaveBeenCalled();
  dialog.querySelector('[data-search-filters-close]').click();
  expect(dialog.hasAttribute('open')).toBe(false);
  expect(document.activeElement).toBe(open);
});

test('a wide screen keeps the facets in the sidebar and takes them back', () => {
  const {dialog, change} = page(false);
  const layout = document.querySelector('.asc-search-layout');
  expect(document.querySelector('[data-search-filters-open]').hidden).toBe(true);
  expect(layout.firstElementChild.className).toBe('asc-search-facets');
  change(true);
  expect(dialog.querySelector('.asc-search-facets')).not.toBeNull();
  dialog.setAttribute('open', '');
  change(false);
  expect(dialog.close).toHaveBeenCalled();
  expect(layout.firstElementChild.className).toBe('asc-search-facets');
});
