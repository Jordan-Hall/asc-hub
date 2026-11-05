import {cardOrder, fill, initAscPins, moveRow, placeOver} from './asc-pins.js';
import {showErrorToast} from '../modules/toast.js';

vi.mock('../modules/toast.js', () => ({showErrorToast: vi.fn()}));

const rows = (names) => names.map((n, i) => `<li class="wk-pin-row" draggable="true" data-id="${i + 1}">
  <div class="ds-check"><input id="pin-${i + 1}" type="checkbox" name="repo" value="${i + 1}"${i === 0 ? ' checked' : ''}><label for="pin-${i + 1}">${n}</label></div>
  <span class="wk-pin-move"><button type="button" data-asc-pin-up>Up</button><button type="button" data-asc-pin-down>Down</button></span>
</li>`).join('');

const page = () => `
<ul class="wk-pins" data-asc-pins-cards>
  <li class="wk-pin" draggable="true" data-id="1">o/a</li>
  <li class="wk-pin" draggable="true" data-id="2">o/b</li>
</ul>
<a href="/user/settings/pins" data-asc-pins-open>Customize</a>
<dialog data-asc-pins-dialog>
  <form action="/user/settings/pins" method="post" data-failed="Not saved." data-asc-pins-form data-asc-pins-inline>
    <input type="hidden" name="_csrf" value="tok">
    <ol data-asc-pins data-moved="%s is now at position %s.">${rows(['o/a', 'o/b', 'o/c'])}</ol>
    <p role="status" data-asc-pins-status></p>
    <p class="tw-hidden" role="alert" data-asc-pins-error></p>
    <button type="submit">Save</button><button type="button" data-asc-pins-close>Cancel</button>
  </form>
</dialog>`;

const q = (sel) => document.querySelector(sel);
const order = (sel) => Array.from(document.querySelectorAll(sel), (el) => el.dataset.id);
const answer = (status, text = '') => Promise.resolve({status, text: () => Promise.resolve(text)});
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

function drag(item, over) {
  item.dispatchEvent(new Event('dragstart', {bubbles: true}));
  over.dispatchEvent(new Event('dragover', {bubbles: true, cancelable: true}));
  over.dispatchEvent(new Event('drop', {bubbles: true, cancelable: true}));
  item.dispatchEvent(new Event('dragend', {bubbles: true}));
}

beforeEach(() => {
  document.body.innerHTML = page();
  const dialog = q('dialog');
  dialog.showModal = vi.fn(() => dialog.setAttribute('open', ''));
  dialog.close = vi.fn(() => dialog.removeAttribute('open'));
});

test('fill puts the arguments in place of the marks in order', () => {
  expect(fill('%s is now at position %s.', 'o/a', 2)).toBe('o/a is now at position 2.');
  expect(fill('no marks', 'x')).toBe('no marks');
});

test('moveRow moves one place and stops at the ends', () => {
  const [a, b] = document.querySelectorAll('[data-asc-pins] > li');
  expect(moveRow(a, -1)).toBe(false);
  expect(moveRow(a, 1)).toBe(true);
  expect(order('[data-asc-pins] > li')).toEqual(['2', '1', '3']);
  expect(moveRow(b, -1)).toBe(false);
});

test('placeOver follows the pointer from above and from below', () => {
  const [a, , c] = document.querySelectorAll('[data-asc-pins] > li');
  expect(placeOver(a, c)).toBe(true);
  expect(order('[data-asc-pins] > li')).toEqual(['2', '3', '1']);
  expect(placeOver(a, document.querySelector('[data-asc-pins] > li'))).toBe(true);
  expect(order('[data-asc-pins] > li')).toEqual(['1', '2', '3']);
  expect(placeOver(a, a)).toBe(false);
  expect(placeOver(a, q('.wk-pin'))).toBe(false);
});

test('Move down moves the row, keeps the focus and announces the place', () => {
  initAscPins({send: vi.fn()});
  const down = document.querySelector('[data-asc-pins] > li [data-asc-pin-down]');
  down.click();
  expect(order('[data-asc-pins] > li')).toEqual(['2', '1', '3']);
  expect(document.activeElement).toBe(down);
  expect(q('[data-asc-pins-status]').textContent).toBe('o/a is now at position 2.');
});

test('a dragged row is announced too', () => {
  initAscPins({send: vi.fn()});
  const [a, , c] = document.querySelectorAll('[data-asc-pins] > li');
  drag(a, c);
  expect(order('[data-asc-pins] > li')).toEqual(['2', '3', '1']);
  expect(q('[data-asc-pins-status]').textContent).toBe('o/a is now at position 3.');
});

test('Customize opens the dialog instead of the settings page, Cancel closes it', () => {
  initAscPins({send: vi.fn()});
  const click = new MouseEvent('click', {bubbles: true, cancelable: true});
  q('[data-asc-pins-open]').dispatchEvent(click);
  expect(click.defaultPrevented).toBe(true);
  expect(q('dialog').showModal).toHaveBeenCalled();
  expect(document.activeElement).toBe(q('input[name="repo"]'));
  q('[data-asc-pins-close]').click();
  expect(q('dialog').close).toHaveBeenCalled();
});

test('the dialog saves inline and reloads the profile', async () => {
  const send = vi.fn(() => answer(204));
  const reload = vi.fn();
  initAscPins({send, reload});
  q('[data-asc-pins-form]').dispatchEvent(new Event('submit', {cancelable: true}));
  await settle();
  const [url, {data}] = send.mock.calls[0];
  expect(url).toBe('/user/settings/pins');
  expect(data.get('inline')).toBe('1');
  expect(data.getAll('repo')).toEqual(['1']);
  expect(reload).toHaveBeenCalled();
});

test('a refused save shows the reason of the hub and stays open', async () => {
  const reload = vi.fn();
  initAscPins({send: () => answer(400, 'You can pin at most 6 repositories.\n'), reload});
  q('[data-asc-pins-form]').dispatchEvent(new Event('submit', {cancelable: true}));
  await settle();
  const error = q('[data-asc-pins-error]');
  expect(error.textContent).toBe('You can pin at most 6 repositories.');
  expect(error.classList.contains('tw-hidden')).toBe(false);
  expect(reload).not.toHaveBeenCalled();
});

test('a save that cannot reach the hub says so', async () => {
  initAscPins({send: () => Promise.reject(new TypeError('Failed to fetch')), reload: vi.fn()});
  q('[data-asc-pins-form]').dispatchEvent(new Event('submit', {cancelable: true}));
  await settle();
  expect(q('[data-asc-pins-error]').textContent).toBe('Not saved.');
});

test('dragging a card saves the new order at once', async () => {
  const send = vi.fn(() => answer(204));
  initAscPins({send});
  const [a, b] = document.querySelectorAll('.wk-pin');
  drag(a, b);
  await settle();
  expect(order('.wk-pin')).toEqual(['2', '1']);
  const [, {data}] = send.mock.calls[0];
  expect(data.get('_csrf')).toBe('tok');
  expect(data.getAll('repo')).toEqual(['2', '1']);
  expect(data.get('inline')).toBe('1');
});

test('a card order the hub refuses is reported', async () => {
  initAscPins({send: () => answer(400)});
  const [a, b] = document.querySelectorAll('.wk-pin');
  drag(a, b);
  await settle();
  expect(showErrorToast).toHaveBeenCalledWith('Not saved.');
});

test('cardOrder sends every card in order', () => {
  const data = cardOrder(q('[data-asc-pins-cards]'), 't');
  expect(Array.from(data.entries())).toEqual([['_csrf', 't'], ['repo', '1'], ['repo', '2'], ['inline', '1']]);
});

test('a page without pins does nothing', () => {
  document.body.innerHTML = '<p>nothing</p>';
  expect(() => initAscPins()).not.toThrow();
});
