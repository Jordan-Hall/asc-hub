import {matches, initAscFeedFilter} from './asc-feed-filter.js';

test('a filter keeps its kind, and the empty filter keeps all', () => {
  expect(matches('push', '')).toBe(true);
  expect(matches('push', 'push')).toBe(true);
  expect(matches('review', 'push')).toBe(false);
});

test('the chips narrow the list and say how many are left', () => {
  document.body.innerHTML = `<section><div data-feed-filter="#feed">
    <button type="button" data-kind="" aria-pressed="true">All</button><button type="button" data-kind="push" aria-pressed="false">Pushes</button></div>
    <p data-feed-count data-template="%d shown"></p>
    <ul id="feed"><li data-feed-item data-kind="push">a</li><li data-feed-item data-kind="review">b</li><li data-feed-item data-kind="push">c</li></ul></section>`;
  initAscFeedFilter();
  document.querySelector('button[data-kind=push]').click();
  const items = Array.from(document.querySelectorAll('#feed li'));
  expect(items.map((i) => i.hidden)).toEqual([false, true, false]);
  expect(document.querySelector('[data-feed-count]').textContent).toBe('2 shown');
  expect(document.querySelector('button[data-kind=push]').getAttribute('aria-pressed')).toBe('true');
  expect(document.querySelector('button[data-kind=""]').getAttribute('aria-pressed')).toBe('false');
  document.querySelector('button[data-kind=""]').click();
  expect(items.every((i) => !i.hidden)).toBe(true);
});
