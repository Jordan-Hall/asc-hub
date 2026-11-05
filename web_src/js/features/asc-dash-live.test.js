import {initAscDashLive, retryDelay} from './asc-dash-live.js';

const card = (items) => `<section data-asc-dash-today="/-/asc/dash/today">
  <div class="ds-card-h"><h2 id="wk-today-title">Today</h2></div>
  <div data-asc-dash-offline hidden>
    <div role="alert">Offline.</div>
    <button type="button" data-asc-dash-retry>Try again</button>
    <span data-asc-dash-countdown data-template="Retrying in %d s"></span>
  </div>
  <div data-feed-filter="#wk-feed"><button type="button" data-kind="" aria-pressed="true">All</button><button type="button" data-kind="push" aria-pressed="false">Pushes</button></div>
  <ul id="wk-feed">${items.map((t) => `<li data-feed-item data-kind="push">${t}</li>`).join('')}</ul>
</section>`;

const ok = (items) => ({ok: true, status: 200, text: () => Promise.resolve(card(items))});
const down = () => Promise.reject(new TypeError('Failed to fetch'));
const q = (sel) => document.querySelector(sel);
const items = () => Array.from(document.querySelectorAll('[data-feed-item]'), (li) => li.textContent);
const settle = () => vi.advanceTimersByTimeAsync(0);

let hidden = false;

beforeEach(() => {
  vi.useFakeTimers();
  hidden = false;
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
  document.body.innerHTML = `<main>${card(['pushed one'])}</main><button id="out">x</button>`;
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

test('each next try waits longer, up to a minute', () => {
  expect([1, 2, 3, 4, 9].map(retryDelay)).toEqual([8, 16, 32, 60, 60]);
});

test('the card loads again every minute and its filter works on the new items', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['pushed two', 'pushed one'])));
  initAscDashLive({load, refreshMs: 60_000});
  await vi.advanceTimersByTimeAsync(59_000);
  expect(load).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(1000);
  expect(load).toHaveBeenCalledWith('/-/asc/dash/today');
  expect(items()).toEqual(['pushed two', 'pushed one']);

  q('[data-kind="push"]').click();
  expect(q('[data-kind="push"]').getAttribute('aria-pressed')).toBe('true');
});

test('a failed load keeps the items and shows the offline state with a countdown, then tries again', async () => {
  const load = vi.fn(down);
  initAscDashLive({load, refreshMs: 1000});
  await vi.advanceTimersByTimeAsync(1000);
  expect(q('[data-asc-dash-offline]').hidden).toBe(false);
  expect(q('[data-asc-dash-countdown]').textContent).toBe('Retrying in 8 s');
  expect(items()).toEqual(['pushed one']);

  await vi.advanceTimersByTimeAsync(1000);
  expect(q('[data-asc-dash-countdown]').textContent).toBe('Retrying in 7 s');
  await vi.advanceTimersByTimeAsync(7000);
  expect(load).toHaveBeenCalledTimes(2);
  expect(q('[data-asc-dash-countdown]').textContent).toBe('Retrying in 16 s');

  load.mockImplementation(() => Promise.resolve(ok(['pushed three'])));
  await vi.advanceTimersByTimeAsync(16_000);
  expect(q('[data-asc-dash-offline]').hidden).toBe(true);
  expect(items()).toEqual(['pushed three']);
});

test('an error status or a page without the card is a failed load', async () => {
  const load = vi.fn(() => Promise.resolve({ok: false, status: 502, text: () => Promise.resolve('')}));
  initAscDashLive({load, refreshMs: 1000});
  await vi.advanceTimersByTimeAsync(1000);
  expect(q('[data-asc-dash-offline]').hidden).toBe(false);

  load.mockImplementation(() => Promise.resolve({ok: true, status: 200, text: () => Promise.resolve('<p>Sign in</p>')}));
  q('[data-asc-dash-retry]').click();
  await settle();
  expect(q('[data-asc-dash-countdown]').textContent).toBe('Retrying in 16 s');
});

test('Try again loads at once and gives the focus to the heading of the new card', async () => {
  const load = vi.fn(down);
  initAscDashLive({load, refreshMs: 1000});
  await vi.advanceTimersByTimeAsync(1000);
  load.mockImplementation(() => Promise.resolve(ok(['back'])));
  q('[data-asc-dash-retry]').click();
  await settle();
  expect(load).toHaveBeenCalledTimes(2);
  expect(items()).toEqual(['back']);
  expect(document.activeElement).toBe(q('#wk-today-title'));
});

test('the connection coming back tries at once', async () => {
  const load = vi.fn(down);
  initAscDashLive({load, refreshMs: 1000});
  await vi.advanceTimersByTimeAsync(1000);
  load.mockImplementation(() => Promise.resolve(ok(['online again'])));
  window.dispatchEvent(new Event('online'));
  await settle();
  expect(items()).toEqual(['online again']);
});

test('a hidden page waits until it is in view; a card in use waits for the next minute', async () => {
  const load = vi.fn(() => Promise.resolve(ok(['new'])));
  initAscDashLive({load, refreshMs: 1000});
  hidden = true;
  await vi.advanceTimersByTimeAsync(5000);
  expect(load).not.toHaveBeenCalled();
  hidden = false;
  document.dispatchEvent(new Event('visibilitychange'));
  await settle();
  expect(load).toHaveBeenCalledTimes(1);

  q('[data-kind="push"]').click();
  q('#out').focus();
  await vi.advanceTimersByTimeAsync(3000);
  expect(load).toHaveBeenCalledTimes(1);
  q('[data-kind=""]').click();
  await vi.advanceTimersByTimeAsync(1000);
  expect(load).toHaveBeenCalledTimes(2);
});

test('a page without the card does nothing', () => {
  document.body.innerHTML = '<p>x</p>';
  const load = vi.fn();
  initAscDashLive({load, refreshMs: 1000});
  vi.advanceTimersByTime(5000);
  expect(load).not.toHaveBeenCalled();
});
