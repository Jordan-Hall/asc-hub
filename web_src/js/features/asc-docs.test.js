import {keepSideScroll, pickActive} from './asc-docs.js';

test('the section in view is the last heading that has reached the top', () => {
  const heads = [{id: 'a', top: -400}, {id: 'b', top: 20}, {id: 'c', top: 500}];
  expect(pickActive(heads)).toBe('b');
  expect(pickActive([{id: 'a', top: 300}, {id: 'b', top: 900}])).toBe('a');
  expect(pickActive([{id: 'a', top: -10}, {id: 'b', top: -5}])).toBe('b');
  expect(pickActive([])).toBeNull();
});

const fail = () => {
  throw new Error('blocked');
};

const memory = (init = {}) => {
  const m = new Map(Object.entries(init));
  return {getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, v), map: m};
};

test('the guides beside an article keep their scroll position from page to page', () => {
  document.body.innerHTML = '<nav data-asc-docs-side><a href="/a">A</a><a href="/b" aria-current="page">B</a></nav>';
  const side = document.querySelector('[data-asc-docs-side]');
  const seen = vi.fn();
  side.querySelector('[aria-current]').scrollIntoView = seen;
  const storage = memory({'asc-docs-side': '120'});
  keepSideScroll(side, storage);
  expect(side.scrollTop).toBe(120);
  expect(seen).toHaveBeenCalledWith({block: 'nearest'});

  side.scrollTop = 40;
  window.dispatchEvent(new Event('pagehide'));
  expect(storage.map.get('asc-docs-side')).toBe('40');
});

test('blocked storage leaves the guides at their top', () => {
  document.body.innerHTML = '<nav data-asc-docs-side><a href="/a">A</a></nav>';
  const side = document.querySelector('[data-asc-docs-side]');
  const blocked = {getItem: fail, setItem: fail};
  expect(() => keepSideScroll(side, blocked)).not.toThrow();
  expect(side.scrollTop).toBe(0);
  expect(() => window.dispatchEvent(new Event('pagehide'))).not.toThrow();
});
