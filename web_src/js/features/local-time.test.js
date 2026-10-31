import {formatLocalTime, formatLocalTitle, applyLocalTime, applyLocalTimes} from './local-time.js';

test('a time is drawn in the zone of the person, in the board formats', () => {
  expect(formatLocalTime('2026-10-27T02:00:00Z', 'daytime', 'UTC')).toBe('27 Oct 02:00');
  expect(formatLocalTime('2026-10-27T02:00:00Z', 'daytime', 'Asia/Tokyo')).toBe('27 Oct 11:00');
  expect(formatLocalTime('2026-10-27T23:30:00Z', 'daytime', 'Asia/Tokyo')).toBe('28 Oct 08:30');
  expect(formatLocalTime('2026-02-03T23:00:00Z', 'day', 'Pacific/Auckland')).toBe('4 Feb 2026');
  expect(formatLocalTime('2026-02-03T10:00:00Z', 'day', 'America/Los_Angeles')).toBe('3 Feb 2026');
  expect(formatLocalTime('not a time', 'day', 'UTC')).toBeNull();
  expect(formatLocalTime('2026-02-03T10:00:00Z', 'other', 'UTC')).toBeNull();
  expect(formatLocalTitle('2026-10-27T02:00:00Z', 'Asia/Tokyo')).toMatch(/^2026-10-27 11:00:00 /);
});

test('elements are rewritten, text only, and an unknown zone changes nothing', () => {
  document.body.innerHTML = '<p id="r"><time id="a" data-local="daytime" datetime="2026-10-27T02:00:00Z" title="x">27 Oct 02:00</time><time id="b" data-local="day" datetime="2026-02-03T10:00:00Z">3 Feb 2026</time><time id="c" datetime="2026-02-03T10:00:00Z">plain</time></p>';
  expect(applyLocalTimes(document, 'Asia/Tokyo')).toBe(2);
  expect(document.getElementById('a').textContent).toBe('27 Oct 11:00');
  expect(document.getElementById('a').getAttribute('title')).toMatch(/^2026-10-27 11:00:00 /);
  expect(document.getElementById('c').textContent).toBe('plain');
  const b = document.getElementById('b');
  expect(applyLocalTime(b, 'Not/AZone')).toBe(false);
  expect(b.textContent).toBe('3 Feb 2026');
});
