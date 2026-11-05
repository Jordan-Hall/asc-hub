import {DELAY, initAscDocsSearch, isSearchKey, resultsURL} from './asc-docs-search.js';

const page = `<form action="/-/docs" method="get"><input type="search" name="q" data-asc-docs-q></form>
<div data-asc-docs-live>
  <p class="ds-sr" role="status" data-asc-docs-status data-template="%d results"></p>
  <div class="site-results-loading" data-asc-docs-loading hidden></div>
</div><button type="button" id="other">other</button>`;
const results = (titles) => `<section data-asc-docs-results>${titles.map((t) => `<a class="site-hit" href="/${t}">${t}</a>`).join('')}</section>`;
const ok = (titles) => ({ok: true, status: 200, text: () => Promise.resolve(results(titles))});
const q = (sel) => document.querySelector(sel);
const hits = () => Array.from(document.querySelectorAll('.site-hit'), (a) => a.textContent);
const press = (key, init = {}, target = window) => {
  const e = new KeyboardEvent('keydown', {key, bubbles: true, cancelable: true, ...init});
  target.dispatchEvent(e);
  return e;
};
const type = (value) => {
  q('[data-asc-docs-q]').value = value;
  q('[data-asc-docs-q]').dispatchEvent(new Event('input'));
};

beforeEach(() => {
  vi.useFakeTimers();
  document.body.innerHTML = page;
});

afterEach(() => {
  vi.useRealTimers();
});

test('Ctrl K, Cmd K and "/" outside a field open the search; "/" inside a field and shifted keys do not', () => {
  const input = document.createElement('input');
  expect(isSearchKey({key: 'k', ctrlKey: true, target: document.body})).toBe(true);
  expect(isSearchKey({key: 'K', metaKey: true, target: input})).toBe(true);
  expect(isSearchKey({key: '/', target: document.body})).toBe(true);
  expect(isSearchKey({key: '/', target: input})).toBe(false);
  expect(isSearchKey({key: 'k', ctrlKey: true, shiftKey: true, target: document.body})).toBe(false);
  expect(isSearchKey({key: 'j', ctrlKey: true, target: document.body})).toBe(false);
});

test('the results address keeps the form action and asks for the results alone', () => {
  expect(resultsURL('/-/docs', 'stack push')).toBe('/-/docs?q=stack+push&fragment=results');
});

test('Ctrl K moves the focus to the search box before the palette sees the key', () => {
  initAscDocsSearch({load: vi.fn()});
  const palette = vi.fn();
  document.addEventListener('keydown', palette);
  q('#other').focus();
  const e = press('k', {ctrlKey: true}, q('#other'));
  expect(e.defaultPrevented).toBe(true);
  expect(document.activeElement).toBe(q('[data-asc-docs-q]'));
  expect(palette).not.toHaveBeenCalled();
  document.removeEventListener('keydown', palette);
});

test('typing loads the results a moment after the last key, with a loading state and a count', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['submit', 'stack'])));
  initAscDocsSearch({load});
  type('sub');
  type('submit');
  expect(load).not.toHaveBeenCalled();
  vi.advanceTimersByTime(DELAY);
  expect(q('[data-asc-docs-loading]').hidden).toBe(false);
  expect(q('[data-asc-docs-live]').getAttribute('aria-busy')).toBe('true');
  await vi.runAllTimersAsync();
  expect(load).toHaveBeenCalledTimes(1);
  expect(load).toHaveBeenCalledWith('/-/docs?q=submit&fragment=results');
  expect(hits()).toEqual(['submit', 'stack']);
  expect(q('[data-asc-docs-status]').textContent).toBe('2 results');
  expect(q('[data-asc-docs-loading]').hidden).toBe(true);
  expect(q('[data-asc-docs-live]').hasAttribute('aria-busy')).toBe(false);
  expect(new URL(window.location.href).searchParams.get('q')).toBe('submit');
});

test('new results replace the old ones, and an emptied box clears them', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['a'])));
  initAscDocsSearch({load});
  type('a');
  await vi.runAllTimersAsync();
  load.mockImplementation(() => Promise.resolve(ok(['b', 'c'])));
  type('b');
  await vi.runAllTimersAsync();
  expect(hits()).toEqual(['b', 'c']);
  expect(document.querySelectorAll('[data-asc-docs-results]')).toHaveLength(1);
  type('  ');
  expect(hits()).toEqual([]);
  expect(q('[data-asc-docs-status]').textContent).toBe('');
});

test('a failed load keeps the results already shown and ends the loading state', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['a'])));
  initAscDocsSearch({load});
  type('a');
  await vi.runAllTimersAsync();
  load.mockImplementation(() => Promise.resolve({ok: false, status: 502, text: () => Promise.resolve('')}));
  type('ab');
  await vi.runAllTimersAsync();
  expect(hits()).toEqual(['a']);
  expect(q('[data-asc-docs-loading]').hidden).toBe(true);
});

test('a page without the search box does nothing', () => {
  document.body.innerHTML = '<p>no docs</p>';
  initAscDocsSearch({load: vi.fn()});
  const e = press('k', {ctrlKey: true});
  expect(e.defaultPrevented).toBe(false);
});
