import {GET} from '../modules/fetch.js';
import {follower, mentionsRepo, reloadStacks} from './asc-stacks-live.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

function row(key, status, title = key) {
  return `<li data-stack-row data-stack-key="${key}" data-status="${status}"><span data-stack-chip>${status}</span> ${title}</li>`;
}

function page(rows) {
  return `<html><body><div data-stack-filter><button data-stack-show="all" aria-pressed="true"></button></div><ul data-stack-rows data-stack-live="7">${rows.join('')}</ul></body></html>`;
}

function mount(rows) {
  document.body.innerHTML = page(rows);
  return document.querySelector('[data-stack-live]');
}

function answer(rows) {
  GET.mockResolvedValue({ok: true, text: () => Promise.resolve(page(rows))});
}

afterEach(() => GET.mockReset());

test('a row whose status changed is swapped and its chip pulses once', async () => {
  const list = mount([row('a', 'review'), row('b', 'queued')]);
  list.querySelector('[data-stack-key="b"]').setAttribute('data-filter-miss', '');
  answer([row('a', 'failing'), row('b', 'queued')]);
  expect(await reloadStacks(list, '/o/r/pulls')).toBe(1);
  const a = list.querySelector('[data-stack-key="a"]');
  expect(a.getAttribute('data-status')).toBe('failing');
  const chip = a.querySelector('[data-stack-chip]');
  expect(chip.hasAttribute('data-stack-pulse')).toBe(true);
  chip.dispatchEvent(new Event('animationend'));
  expect(chip.hasAttribute('data-stack-pulse')).toBe(false);
  expect(list.querySelector('[data-stack-key="b"]').hasAttribute('data-filter-miss'), 'a filtered row that did not change stays as it is').toBe(true);
});

test('a row that changed without a new status is swapped without a pulse', async () => {
  const list = mount([row('a', 'review', 'old intent')]);
  answer([row('a', 'review', 'new intent')]);
  expect(await reloadStacks(list, '/o/r/pulls')).toBe(0);
  const a = list.querySelector('[data-stack-key="a"]');
  expect(a.textContent).toContain('new intent');
  expect(a.querySelector('[data-stack-pulse]')).toBeNull();
});

test('a stack that came or went replaces the rows whole', async () => {
  const list = mount([row('a', 'review')]);
  answer([row('a', 'review'), row('c', 'draft')]);
  expect(await reloadStacks(list, '/o/r/pulls')).toBe(0);
  expect(Array.from(list.querySelectorAll('[data-stack-row]'), (r) => r.getAttribute('data-stack-key'))).toEqual(['a', 'c']);
  expect(list.querySelector('[data-stack-pulse]')).toBeNull();
});

test('a failed load or a page without this list changes nothing', async () => {
  const list = mount([row('a', 'review')]);
  GET.mockResolvedValue({ok: false});
  expect(await reloadStacks(list, '/o/r/pulls')).toBe(0);
  GET.mockResolvedValue({ok: true, text: () => Promise.resolve('<html><body><ul data-stack-live="8"></ul></body></html>')});
  expect(await reloadStacks(list, '/o/r/pulls')).toBe(0);
  expect(list.querySelectorAll('[data-stack-row]')).toHaveLength(1);
});

test('only an event naming this repository reloads', () => {
  expect(mentionsRepo('[3,7]', 7)).toBe(true);
  expect(mentionsRepo('[3]', 7)).toBe(false);
  expect(mentionsRepo('not json', 7)).toBe(false);
  expect(mentionsRepo('{"7":1}', 7)).toBe(false);
});

test('events during a load ask for one more load, not one each', async () => {
  let release;
  const reload = vi.fn(() => new Promise((resolve) => {
    release = resolve;
  }));
  const run = follower(reload);
  const first = run();
  run();
  run();
  expect(reload).toHaveBeenCalledTimes(1);
  release();
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(reload).toHaveBeenCalledTimes(2);
  release();
  await first;
  expect(reload).toHaveBeenCalledTimes(2);
});
