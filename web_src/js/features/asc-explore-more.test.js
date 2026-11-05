import {initAscExploreMore} from './asc-explore-more.js';

const rows = (names) => names.map((n) => `<li class="ds-row"><a href="/${n}">${n}</a></li>`).join('');
const more = (next) => (next ? `<div data-asc-more=".ds-repolist" data-asc-more-next="${next}" hidden>
  <button type="button" data-asc-more-btn>Load more</button>
  <p role="status" data-asc-more-status data-template="%d more loaded" data-failed="More could not load. Try again."></p>
</div>` : '');
const page = (names, next) => `<div class="ds-explore-body"><ul class="ds-repolist">${rows(names)}</ul>
  <div class="ds-pager-wrap">pages</div>${more(next)}</div>`;

const ok = (names, next) => ({ok: true, status: 200, text: () => Promise.resolve(page(names, next))});
const q = (sel) => document.querySelector(sel);
const names = () => Array.from(document.querySelectorAll('.ds-repolist a'), (a) => a.textContent);
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

let observers = [];
class FakeObserver {
  constructor(callback, options) {
    this.callback = callback;
    this.options = options;
    this.watched = new Set();
    observers.push(this);
  }
  observe(el) {
    this.watched.add(el);
  }
  unobserve(el) {
    this.watched.delete(el);
  }
  disconnect() {
    this.watched.clear();
  }
  cross() {
    this.callback(Array.from(this.watched, (target) => ({target, isIntersecting: true})));
  }
}

const fakeWin = ({phone = true, observer = true, saveData = false} = {}) => ({
  matchMedia: () => ({matches: phone}),
  navigator: {connection: {saveData}},
  ...(observer ? {IntersectionObserver: FakeObserver} : {}),
});

beforeEach(() => {
  observers = [];
  document.body.innerHTML = page(['a', 'b'], '/explore/repos?page=2');
});

test('on a desktop the pager stays and nothing loads', () => {
  const load = vi.fn();
  initAscExploreMore({win: fakeWin({phone: false}), load});
  expect(q('[data-asc-more]').hidden).toBe(true);
  expect(q('.ds-pager-wrap').hidden).toBe(false);
  expect(observers).toHaveLength(0);
});

test('on a phone the pager gives way and the next page loads as the end of the list comes near', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['c', 'd'], '/explore/repos?page=3')));
  initAscExploreMore({win: fakeWin(), load});
  expect(q('[data-asc-more]').hidden).toBe(false);
  expect(q('.ds-pager-wrap').hidden).toBe(true);
  expect(observers[0].options.rootMargin).toBe('0px 0px 400px 0px');

  observers[0].cross();
  await settle();
  expect(load).toHaveBeenCalledWith('/explore/repos?page=2');
  expect(names()).toEqual(['a', 'b', 'c', 'd']);
  expect(q('[data-asc-more-status]').textContent).toBe('2 more loaded');
  expect(q('[data-asc-more]').hasAttribute('aria-busy')).toBe(false);

  observers[0].cross();
  await settle();
  expect(load).toHaveBeenLastCalledWith('/explore/repos?page=3');
});

test('Load more loads the next page and gives the focus to the first new row', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['c'], '/explore/repos?page=3')));
  initAscExploreMore({win: fakeWin({observer: false}), load});
  q('[data-asc-more-btn]').click();
  await settle();
  expect(names()).toEqual(['a', 'b', 'c']);
  expect(document.activeElement).toBe(q('a[href="/c"]'));
  expect(q('[data-asc-more-btn]').hidden).toBe(false);
});

test('the last page hides the button and stops watching', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['c'], '')));
  initAscExploreMore({win: fakeWin(), load});
  observers[0].cross();
  await settle();
  expect(q('[data-asc-more-btn]').hidden).toBe(true);
  expect(observers[0].watched.size).toBe(0);
  q('[data-asc-more-btn]').click();
  await settle();
  expect(load).toHaveBeenCalledTimes(1);
});

test('a failed load keeps the rows and the button, says so, and stops loading by itself', async () => {
  const load = vi.fn(() => Promise.resolve({ok: false, status: 502, text: () => Promise.resolve('')}));
  initAscExploreMore({win: fakeWin(), load});
  observers[0].cross();
  await settle();
  expect(names()).toEqual(['a', 'b']);
  expect(q('[data-asc-more-btn]').hidden).toBe(false);
  expect(q('[data-asc-more-status]').textContent).toBe('More could not load. Try again.');
  expect(observers[0].watched.size).toBe(0);

  load.mockImplementation(() => Promise.resolve(ok(['c'], '')));
  q('[data-asc-more-btn]').click();
  await settle();
  expect(names()).toEqual(['a', 'b', 'c']);
});

test('with data saver on only Load more loads', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['c'], '/explore/repos?page=3')));
  initAscExploreMore({win: fakeWin({saveData: true}), load});
  expect(observers).toHaveLength(0);
  expect(q('[data-asc-more]').hidden).toBe(false);
  q('[data-asc-more-btn]').click();
  await settle();
  expect(load).toHaveBeenCalledTimes(1);
});

test('a page without more pages does nothing', () => {
  document.body.innerHTML = page(['a'], '');
  const load = vi.fn();
  initAscExploreMore({win: fakeWin(), load});
  expect(q('.ds-pager-wrap').hidden).toBe(false);
  expect(observers).toHaveLength(0);
});
