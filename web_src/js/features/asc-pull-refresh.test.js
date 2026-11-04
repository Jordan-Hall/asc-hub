import {PULL_ARM, initAscPullRefresh, pullOffset} from './asc-pull-refresh.js';

function page(standalone) {
  document.body.innerHTML = '<div class="rv-ptr" data-asc-ptr></div><div data-asc-pull-refresh id="area"><p id="row">log</p></div>';
  const win = {scrollY: 0, matchMedia: () => ({matches: standalone}), location: {reload: vi.fn()}};
  initAscPullRefresh(document, win);
  return win;
}

const touch = (type, y) => {
  const ev = new Event(type, {bubbles: true});
  ev.touches = type === 'touchend' || type === 'touchcancel' ? [] : [{clientY: y}];
  document.querySelector('#row').dispatchEvent(ev);
};

test('the arrow comes down half as far as the finger and stops', () => {
  expect(pullOffset(-20)).toBe(0);
  expect(pullOffset(60)).toBe(30);
  expect(pullOffset(1000)).toBe(110);
});

test('in the installed app a pull from the top past the mark reloads the page', () => {
  const win = page(true);
  touch('touchstart', 100);
  touch('touchmove', 100 + PULL_ARM * 2 + 4);
  expect(document.querySelector('[data-asc-ptr]').classList.contains('is-armed')).toBe(true);
  touch('touchend');
  expect(win.location.reload).toHaveBeenCalledTimes(1);
});

test('a short pull, a pull below the top or a browser tab does not reload', () => {
  let win = page(true);
  touch('touchstart', 100);
  touch('touchmove', 140);
  touch('touchend');
  expect(win.location.reload).not.toHaveBeenCalled();
  expect(document.querySelector('[data-asc-ptr]').style.transform).toBe('');

  win = page(true);
  win.scrollY = 300;
  touch('touchstart', 100);
  touch('touchmove', 400);
  touch('touchend');
  expect(win.location.reload).not.toHaveBeenCalled();

  win = page(false);
  touch('touchstart', 100);
  touch('touchmove', 400);
  touch('touchend');
  expect(win.location.reload).not.toHaveBeenCalled();
});
