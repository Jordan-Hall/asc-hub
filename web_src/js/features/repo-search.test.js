import {initRepositorySearch} from './repo-search.js';

test('a changed sort shows the skeleton of the list before the page loads again', () => {
  document.body.innerHTML = `<form id="repo-search-form" data-asc-list-filter>
    <input name="q" value="pay"><select name="sort"><option value="newest">n</option><option value="alphabetically" selected>a</option></select>
  </form>
  <div data-asc-list-main><div data-asc-list-loading hidden></div><ul id="rows"></ul></div>`;
  const here = window.location.href;
  const assigned = [];
  const location = {
    href: here,
    get search() { return '' },
    set search(v) { assigned.push(v) },
  };
  vi.spyOn(window, 'location', 'get').mockReturnValue(location);
  initRepositorySearch();
  document.querySelector('select').dispatchEvent(new Event('change', {bubbles: true}));
  const main = document.querySelector('[data-asc-list-main]');
  expect(main.classList.contains('is-loading')).toBe(true);
  expect(main.querySelector('[data-asc-list-loading]').hidden).toBe(false);
  expect(assigned).toEqual(['q=pay&sort=alphabetically']);
  vi.restoreAllMocks();
});
