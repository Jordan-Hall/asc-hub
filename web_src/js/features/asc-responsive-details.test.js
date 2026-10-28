import {bindResponsiveDetails, initAscResponsiveDetails} from './asc-responsive-details.js';

function fakeMedia(matches) {
  const listeners = [];
  return {
    matches,
    addEventListener: (_, fn) => listeners.push(fn),
    set(v) {
      this.matches = v;
      for (const fn of listeners) fn();
    },
  };
}

afterEach(() => {
  document.body.innerHTML = '';
});

test('the details is open from the width up and closed below it', () => {
  document.body.innerHTML = '<details id="d" data-asc-open-from="640"><summary>x</summary>y</details>';
  const el = document.querySelector('#d');
  const mq = fakeMedia(false);
  bindResponsiveDetails(el, mq);
  expect(el.open).toBe(false);
  mq.set(true);
  expect(el.open).toBe(true);
  mq.set(false);
  expect(el.open).toBe(false);
});

test('init ignores a bad width and a click outside closes an open menu on a phone', () => {
  window.matchMedia = () => ({matches: false, addEventListener() {}});
  document.body.innerHTML = '<details id="a" data-asc-open-from="x"><summary>x</summary></details><details id="b" data-asc-open-from="640"><summary>x</summary><a id="in">in</a></details><p id="out">o</p>';
  initAscResponsiveDetails();
  const b = document.querySelector('#b');
  b.open = true;
  document.querySelector('#in').click();
  expect(b.open).toBe(true);
  document.querySelector('#out').click();
  expect(b.open).toBe(false);
});
