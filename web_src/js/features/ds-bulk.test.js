import {initBulk, selectionLabel} from './ds-bulk.js';

test('the count says one or many', () => {
  expect(selectionLabel(1, '1 selected', '%d selected')).toBe('1 selected');
  expect(selectionLabel(3, '1 selected', '%d selected')).toBe('3 selected');
});

test('the bar follows the checked rows and Clear unchecks them', () => {
  document.body.innerHTML = `<div data-bulk>
    <ul><li data-bulk-row><input type="checkbox" data-bulk-item value="1"></li><li data-bulk-row><input type="checkbox" data-bulk-item value="2"></li></ul>
    <div data-bulk-bar hidden><span data-bulk-count data-one="1 selected" data-n="%d selected"></span><button data-bulk-clear>Clear</button></div></div>`;
  initBulk();
  const bar = document.querySelector('[data-bulk-bar]');
  const boxes = document.querySelectorAll('input');
  expect(bar.hidden).toBe(true);
  boxes[0].checked = true;
  boxes[0].dispatchEvent(new Event('change', {bubbles: true}));
  expect(bar.hidden).toBe(false);
  expect(document.querySelector('[data-bulk-count]').textContent).toBe('1 selected');
  boxes[1].checked = true;
  boxes[1].dispatchEvent(new Event('change', {bubbles: true}));
  expect(document.querySelector('[data-bulk-count]').textContent).toBe('2 selected');
  document.querySelector('[data-bulk-clear]').click();
  expect(bar.hidden).toBe(true);
  expect(boxes[0].closest('li').classList.contains('is-checked')).toBe(false);
});

test('a bulk action posts once per chosen row with the token', async () => {
  document.body.innerHTML = `<input type="hidden" name="_csrf" value="tok"><div data-bulk>
    <input type="checkbox" data-bulk-item value="7" checked><input type="checkbox" data-bulk-item value="8" checked><input type="checkbox" data-bulk-item value="9">
    <div data-bulk-bar><span data-bulk-count data-one="1" data-n="%d"></span>
    <button data-bulk-do data-bulk-url="/-/asc/inbox/state" data-bulk-field="pull" data-bulk-fields='{"do":"pin"}'>Pin</button></div></div>`;
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
  expect(calls).toEqual([['/-/asc/inbox/state', '_csrf=tok&pull=7&do=pin'], ['/-/asc/inbox/state', '_csrf=tok&pull=8&do=pin']]);
  expect(reload).toHaveBeenCalled();
});
