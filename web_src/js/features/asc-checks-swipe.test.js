import {SWIPE_ARM, initAscChecksSwipe, swipeOffset} from './asc-checks-swipe.js';

const pointer = (type, el, x, y = 0, pointerType = 'touch') => el.dispatchEvent(new PointerEvent(type, {bubbles: true, pointerType, pointerId: 1, clientX: x, clientY: y}));

function groups() {
  document.body.innerHTML = `<ul data-testid="asc-groups">
    <li class="rv-group" id="g1"><div class="rv-li-title" id="t1">Panic</div><a href="?open=1&act=acknowledged" data-testid="asc-group-ack" id="ack">Acknowledge</a></li>
    <li class="rv-group" id="g2"><div class="rv-li-title" id="t2">Resolved</div></li>
  </ul>`;
  const ack = document.querySelector('#ack');
  const clicked = vi.fn((e) => e.preventDefault());
  ack.addEventListener('click', clicked);
  initAscChecksSwipe();
  return clicked;
}

test('a card moves only to the left and no further than the end', () => {
  expect(swipeOffset(30)).toBe(0);
  expect(swipeOffset(-50)).toBe(-50);
  expect(swipeOffset(-500)).toBe(-120);
});

test('a swipe past the mark acknowledges and the card comes back', () => {
  const clicked = groups();
  const title = document.querySelector('#t1');
  pointer('pointerdown', title, 200);
  pointer('pointermove', title, 180);
  pointer('pointermove', title, 200 - SWIPE_ARM - 5);
  expect(document.querySelector('#g1').dataset.swipe).toBe('armed');
  pointer('pointerup', title, 200 - SWIPE_ARM - 5);
  expect(clicked).toHaveBeenCalledTimes(1);
  expect(document.querySelector('#g1').style.transform).toBe('');
  expect(document.querySelector('#g1').dataset.swipe).toBeUndefined();
});

test('a short swipe, a scroll, a mouse or a card without Acknowledge does nothing', () => {
  const clicked = groups();
  const title = document.querySelector('#t1');
  pointer('pointerdown', title, 200);
  pointer('pointermove', title, 160);
  pointer('pointerup', title, 160);
  pointer('pointerdown', title, 200, 0);
  pointer('pointermove', title, 195, 40);
  pointer('pointermove', title, 60, 40);
  pointer('pointerup', title, 60, 40);
  pointer('pointerdown', title, 200, 0, 'mouse');
  pointer('pointermove', title, 60, 0, 'mouse');
  pointer('pointerup', title, 60, 0, 'mouse');
  const other = document.querySelector('#t2');
  pointer('pointerdown', other, 200);
  pointer('pointermove', other, 60);
  pointer('pointerup', other, 60);
  expect(clicked).not.toHaveBeenCalled();
  expect(document.querySelector('#g2').style.transform).toBe('');
});

test('a cancelled swipe puts the card back without acknowledging', () => {
  const clicked = groups();
  const title = document.querySelector('#t1');
  pointer('pointerdown', title, 200);
  pointer('pointermove', title, 60);
  pointer('pointercancel', title, 60);
  expect(clicked).not.toHaveBeenCalled();
  expect(document.querySelector('#g1').style.transform).toBe('');
});
