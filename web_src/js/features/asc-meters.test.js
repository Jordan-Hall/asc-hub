import {clampPct, fillMeters} from './asc-meters.js';

test('clampPct keeps a percentage between 0 and 100 and ignores junk', () => {
  expect(clampPct('72')).toBe(72);
  expect(clampPct('140')).toBe(100);
  expect(clampPct('-3')).toBe(0);
  expect(clampPct('x')).toBe(0);
  expect(clampPct(null)).toBe(0);
});

test('fillMeters sets the width of each bar from data-pct', async () => {
  document.body.innerHTML = '<span id="a" data-pct="40"></span><span id="b" data-pct="999"></span><span id="c" data-pct="oops"></span>';
  expect(fillMeters()).toBe(3);
  await new Promise((resolve) => requestAnimationFrame(resolve));
  expect(document.querySelector('#a').style.width).toBe('40%');
  expect(document.querySelector('#b').style.width).toBe('100%');
  expect(document.querySelector('#c').style.width).toBe('0%');
});
