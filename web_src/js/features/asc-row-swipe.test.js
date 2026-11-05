import {SWIPE_ARM, initAscRowSwipe, swipeAction, swipeOffset} from './asc-row-swipe.js';
import {initAscKeys} from './asc-keys.js';

const pointer = (type, el, x, y = 0, pointerType = 'touch') => el.dispatchEvent(new PointerEvent(type, {bubbles: true, pointerType, pointerId: 1, clientX: x, clientY: y}));
const q = (sel) => document.querySelector(sel);

function rows() {
  document.body.innerHTML = `<ul data-asc-list data-asc-swipe>
    <li id="r1" data-asc-swipe-row><form data-asc-row-state hidden><input name="pull" value="7"><input name="do" value="done"></form><a href="/o/r/pulls/1" id="t1" data-asc-row>One</a></li>
    <li id="r2"><a href="/agent" id="t2" data-asc-row>Agent run</a></li>
  </ul>`;
  const sent = [];
  q('form').addEventListener('submit', (e) => {
    e.preventDefault();
    sent.push(new FormData(e.target).get('do'));
  });
  initAscRowSwipe();
  return sent;
}

const swipe = (el, dx) => {
  pointer('pointerdown', el, 200);
  pointer('pointermove', el, 200 + Math.sign(dx) * 20);
  pointer('pointermove', el, 200 + dx);
  pointer('pointerup', el, 200 + dx);
};

test('a row moves either way, no further than the end, and acts past the mark', () => {
  expect(swipeOffset(500)).toBe(120);
  expect(swipeOffset(-500)).toBe(-120);
  expect(swipeAction(SWIPE_ARM)).toBe('done');
  expect(swipeAction(-SWIPE_ARM)).toBe('snooze');
  expect(swipeAction(30)).toBe('');
});

test('right marks done, left snoozes, and the row comes back', () => {
  const sent = rows();
  swipe(q('#t1'), SWIPE_ARM + 5);
  swipe(q('#t1'), -SWIPE_ARM - 5);
  expect(sent).toEqual(['done', 'snooze']);
  expect(q('#r1').style.transform).toBe('');
  expect(q('#r1').dataset.swipe).toBeUndefined();
});

test('the edge says which action a swipe will take', () => {
  rows();
  pointer('pointerdown', q('#t1'), 200);
  pointer('pointermove', q('#t1'), 220);
  pointer('pointermove', q('#t1'), 200 + SWIPE_ARM + 5);
  expect(q('#r1').dataset.swipe).toBe('done');
  pointer('pointermove', q('#t1'), 200 - SWIPE_ARM - 5);
  expect(q('#r1').dataset.swipe).toBe('snooze');
  pointer('pointercancel', q('#t1'), 200 - SWIPE_ARM - 5);
});

test('a short swipe, a scroll, a mouse or a row without a form does nothing', () => {
  const sent = rows();
  swipe(q('#t1'), 40);
  pointer('pointerdown', q('#t1'), 200, 0);
  pointer('pointermove', q('#t1'), 205, 60);
  pointer('pointerup', q('#t1'), 300, 60);
  pointer('pointerdown', q('#t1'), 200, 0, 'mouse');
  pointer('pointermove', q('#t1'), 320, 0, 'mouse');
  pointer('pointerup', q('#t1'), 320, 0, 'mouse');
  swipe(q('#t2'), SWIPE_ARM + 5);
  expect(sent).toEqual([]);
});

test('e on a row that has the focus marks that row done', () => {
  const sent = rows();
  initAscKeys();
  q('#t1').focus();
  document.dispatchEvent(new KeyboardEvent('keydown', {key: 'e', bubbles: true}));
  expect(sent).toEqual(['done']);
});
