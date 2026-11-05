import {initBulk, selectionLabel} from './ds-bulk.js';

test('the count says one or many', () => {
  expect(selectionLabel(1, '1 selected', '%d selected')).toBe('1 selected');
  expect(selectionLabel(3, '1 selected', '%d selected')).toBe('3 selected');
});

const twoRows = `<div data-bulk>
    <ul><li data-bulk-row><input type="checkbox" data-bulk-item value="1"></li><li data-bulk-row><input type="checkbox" data-bulk-item value="2"></li></ul>
    <div data-bulk-bar hidden><input type="checkbox" checked data-bulk-unselect><b data-bulk-count data-one="1 selected" data-n="%d selected"></b></div></div>`;
const tick = (box) => {
  box.checked = true;
  box.dispatchEvent(new Event('change', {bubbles: true}));
};

test('the bar follows the checked rows and its ticked box clears them', () => {
  document.body.innerHTML = twoRows;
  initBulk();
  const bar = document.querySelector('[data-bulk-bar]');
  const boxes = document.querySelectorAll('input[data-bulk-item]');
  expect(bar.hidden).toBe(true);
  tick(boxes[0]);
  expect(bar.hidden).toBe(false);
  expect(document.querySelector('[data-bulk-count]').textContent).toBe('1 selected');
  tick(boxes[1]);
  expect(document.querySelector('[data-bulk-count]').textContent).toBe('2 selected');
  document.querySelector('[data-bulk-unselect]').click();
  expect(bar.hidden).toBe(true);
  expect(Array.from(boxes, (b) => b.checked)).toEqual([false, false]);
  expect(document.querySelector('[data-bulk-unselect]').checked).toBe(true);
  expect(boxes[0].closest('li').classList.contains('is-checked')).toBe(false);
});

test('Escape in the list clears the selection', () => {
  document.body.innerHTML = twoRows;
  initBulk();
  const boxes = document.querySelectorAll('input[data-bulk-item]');
  tick(boxes[0]);
  boxes[0].dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
  expect(boxes[0].checked).toBe(false);
  expect(document.querySelector('[data-bulk-bar]').hidden).toBe(true);
});

test('a bulk action posts once per chosen row with the token', async () => {
  document.body.innerHTML = `<input type="hidden" name="_csrf" value="tok"><div data-bulk>
    <input type="checkbox" data-bulk-item value="7" checked><input type="checkbox" data-bulk-item value="8" checked><input type="checkbox" data-bulk-item value="9">
    <div data-bulk-bar><span data-bulk-count data-one="1" data-n="%d"></span>
    <button data-bulk-do data-bulk-url="/-/asc/inbox/state" data-bulk-field="pull" data-bulk-fields='{"do":"unwatch"}'>Unwatch</button></div></div>`;
  const calls = [];
  globalThis.fetch = vi.fn(async (url, opts) => {
    calls.push([url, opts.body.toString()]);
    return {ok: true};
  });
  const reload = vi.fn();
  Object.defineProperty(window, 'location', {value: {...window.location, reload}, writable: true});
  initBulk();
  document.querySelector('[data-bulk-do]').click();
  await new Promise((r) => setTimeout(r, 10));
  expect(calls).toEqual([['/-/asc/inbox/state', '_csrf=tok&pull=7&do=unwatch'], ['/-/asc/inbox/state', '_csrf=tok&pull=8&do=unwatch']]);
  expect(reload).toHaveBeenCalled();
});
