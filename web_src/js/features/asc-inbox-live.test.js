import {countLabel, initAscInboxLive, mergeRows, refreshInbox} from './asc-inbox-live.js';

const row = (key, text = key) => `<li class="wk-irow" data-asc-key="${key}"><a data-asc-row href="#">${text}</a></li>`;
const day = (name) => `<li class="wk-iday" data-asc-key="day:${name}">${name}</li>`;
const page = (rows) => `<ul class="wk-ilist" data-asc-list data-asc-inbox-live="/-/asc/inbox/rows?tab=all">${rows.join('')}</ul>
  <p class="ds-sr" role="status" data-asc-inbox-news data-one="1 new item in the inbox" data-n="%d new items in the inbox"></p>`;
const answer = (rows) => ({ok: true, status: 200, text: () => Promise.resolve(`<ul data-asc-inbox-rows>${rows.join('')}</ul>`)});
const keys = () => Array.from(document.querySelectorAll('[data-asc-inbox-live] > li'), (li) => li.getAttribute('data-asc-key'));
const list = () => document.querySelector('[data-asc-inbox-live]');

let hidden = false;

beforeEach(() => {
  vi.useFakeTimers();
  hidden = false;
  vi.spyOn(document, 'hidden', 'get').mockImplementation(() => hidden);
  document.body.innerHTML = page([day('Today'), row('pull:5'), row('pull:4')]);
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

test('count labels', () => {
  expect(countLabel(1, 'one', '%d many')).toBe('one');
  expect(countLabel(3, 'one', '%d many')).toBe('3 many');
});

test('new rows go where the server lists them, the rows shown keep their order', () => {
  const fresh = document.createElement('ul');
  fresh.innerHTML = [day('Today'), row('pull:9'), row('pull:5'), row('run:/r/1'), row('pull:4'), day('Yesterday'), row('pull:2')].join('');
  const added = mergeRows(list(), fresh);
  expect(added.map((el) => el.getAttribute('data-asc-key'))).toEqual(['pull:9', 'run:/r/1', 'day:Yesterday', 'pull:2']);
  expect(keys()).toEqual(['day:Today', 'pull:9', 'pull:5', 'run:/r/1', 'pull:4', 'day:Yesterday', 'pull:2']);
});

test('a new day heading comes in above the rows of the older day', () => {
  document.body.innerHTML = page([day('Yesterday'), row('pull:5')]);
  const fresh = document.createElement('ul');
  fresh.innerHTML = [day('Today'), row('pull:9'), day('Yesterday'), row('pull:5')].join('');
  mergeRows(list(), fresh);
  expect(keys()).toEqual(['day:Today', 'pull:9', 'day:Yesterday', 'pull:5']);
});

test('rows that left the server list stay until the person reloads', () => {
  const fresh = document.createElement('ul');
  fresh.innerHTML = [day('Today'), row('pull:5')].join('');
  expect(mergeRows(list(), fresh)).toEqual([]);
  expect(keys()).toEqual(['day:Today', 'pull:5', 'pull:4']);
});

test('the new rows are marked for a while and the live region counts the items', async () => {
  const load = vi.fn(() => Promise.resolve(answer([day('Today'), row('pull:9'), row('pull:8'), row('pull:5'), row('pull:4')])));
  const n = await refreshInbox(list(), {load});
  expect(n).toBe(2);
  expect(load).toHaveBeenCalledWith('/-/asc/inbox/rows?tab=all', {cache: 'no-store'});
  expect(document.querySelector('[data-asc-key="pull:9"]').classList.contains('is-new')).toBe(true);
  expect(document.querySelector('[data-asc-key="pull:5"]').classList.contains('is-new')).toBe(false);
  expect(document.querySelector('[data-asc-inbox-news]').textContent).toBe('2 new items in the inbox');
  await vi.advanceTimersByTimeAsync(6000);
  expect(document.querySelector('.is-new')).toBeNull();
});

test('when the person scrolled down, what they read stays in place', async () => {
  // the heading is above the window, pull:5 is the first row in view
  const tops = new Map([['pull:5', 20], ['pull:4', 70]]);
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function () {
    const key = this.getAttribute('data-asc-key');
    // a row that came in above them pushes them down by 50
    const shift = key !== 'day:Today' && document.querySelector('[data-asc-key="pull:7"]') ? 50 : 0;
    const top = (tops.get(key) ?? -100) + shift;
    return {top, bottom: top + 50, left: 0, right: 0, width: 0, height: 50};
  });
  const scrollBy = vi.spyOn(window, 'scrollBy').mockImplementation(() => {});
  await refreshInbox(list(), {load: () => Promise.resolve(answer([day('Today'), row('pull:7'), row('pull:5'), row('pull:4')]))});
  expect(keys()).toEqual(['day:Today', 'pull:7', 'pull:5', 'pull:4']);
  expect(scrollBy).toHaveBeenCalledWith(0, 50);
});

test('at the top of the list the new rows are seen coming in', async () => {
  vi.spyOn(Element.prototype, 'getBoundingClientRect').mockReturnValue({top: 10, bottom: 60, left: 0, right: 0, width: 0, height: 50});
  const scrollBy = vi.spyOn(window, 'scrollBy').mockImplementation(() => {});
  await refreshInbox(list(), {load: () => Promise.resolve(answer([day('Today'), row('pull:9'), row('pull:5'), row('pull:4')]))});
  expect(scrollBy).not.toHaveBeenCalled();
});

test('nothing changes while a row is being swiped, or when the load fails', async () => {
  document.querySelector('[data-asc-key="pull:5"]').classList.add('is-swiping');
  const load = vi.fn();
  expect(await refreshInbox(list(), {load})).toBe(0);
  expect(load).not.toHaveBeenCalled();
  document.querySelector('.is-swiping').classList.remove('is-swiping');
  expect(await refreshInbox(list(), {load: () => Promise.resolve({ok: false, status: 500, text: () => Promise.resolve('')})})).toBe(0);
  expect(keys()).toEqual(['day:Today', 'pull:5', 'pull:4']);
});

test('an empty inbox offers to show the items that came in', async () => {
  document.body.innerHTML = `<div class="ds-empty" data-asc-inbox-live="/-/asc/inbox/rows"><h3>Nothing</h3>
    <a data-asc-inbox-more data-one="Show 1 new item" data-n="Show %d new items" href="/-/asc/inbox" hidden></a></div>`;
  const more = document.querySelector('[data-asc-inbox-more]');
  expect(await refreshInbox(list(), {load: () => Promise.resolve(answer([]))})).toBe(0);
  expect(more.hidden).toBe(true);
  expect(await refreshInbox(list(), {load: () => Promise.resolve(answer([day('Today'), row('pull:9'), row('pull:8')]))})).toBe(2);
  expect(more.hidden).toBe(false);
  expect(more.textContent).toBe('Show 2 new items');
  expect(more.getAttribute('href')).toBe(window.location.href);
});

test('it loads every minute while the page is in view, and on coming back', async () => {
  const load = vi.fn(() => Promise.resolve(answer([day('Today'), row('pull:5'), row('pull:4')])));
  initAscInboxLive({load, refreshMs: 1000});
  await vi.advanceTimersByTimeAsync(1000);
  expect(load).toHaveBeenCalledTimes(1);
  await vi.advanceTimersByTimeAsync(1000);
  expect(load).toHaveBeenCalledTimes(2);
  hidden = true;
  await vi.advanceTimersByTimeAsync(5000);
  expect(load).toHaveBeenCalledTimes(2);
  hidden = false;
  document.dispatchEvent(new Event('visibilitychange'));
  await vi.advanceTimersByTimeAsync(0);
  expect(load).toHaveBeenCalledTimes(3);
});

test('a failed load tries again the next minute', async () => {
  const load = vi.fn().mockRejectedValueOnce(new TypeError('Failed to fetch')).mockResolvedValue(answer([day('Today'), row('pull:6'), row('pull:5'), row('pull:4')]));
  initAscInboxLive({load, refreshMs: 1000});
  await vi.advanceTimersByTimeAsync(1000);
  expect(keys()).toEqual(['day:Today', 'pull:5', 'pull:4']);
  await vi.advanceTimersByTimeAsync(1000);
  expect(keys()).toEqual(['day:Today', 'pull:6', 'pull:5', 'pull:4']);
});
