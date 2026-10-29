import {applyScrollFocus, needsFocus} from './asc-scroll-focus.js';

function box(cls, sw, cw) {
  document.body.innerHTML = `<div class="${cls}" id="b"></div>`;
  const el = document.getElementById('b');
  Object.defineProperty(el, 'scrollWidth', {value: sw, configurable: true});
  Object.defineProperty(el, 'clientWidth', {value: cw, configurable: true});
  Object.defineProperty(el, 'scrollHeight', {value: 10, configurable: true});
  Object.defineProperty(el, 'clientHeight', {value: 10, configurable: true});
  return el;
}

test('needsFocus is true only when the content is wider than the box', () => {
  expect(needsFocus(box('x', 500, 300))).toBe(true);
  expect(needsFocus(box('x', 300, 300))).toBe(false);
});

test('a scrolling table gets tabindex 0 and loses it when it fits again', () => {
  const el = box('site-table', 500, 300);
  applyScrollFocus();
  expect(el.getAttribute('tabindex')).toBe('0');
  Object.defineProperty(el, 'scrollWidth', {value: 200, configurable: true});
  applyScrollFocus();
  expect(el.hasAttribute('tabindex')).toBe(false);
});

test('an element that already has a tabindex is left alone', () => {
  const el = box('site-table', 500, 300);
  el.setAttribute('tabindex', '-1');
  applyScrollFocus();
  expect(el.getAttribute('tabindex')).toBe('-1');
});
