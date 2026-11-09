import {formatLocalTime, formatLocalTitle, applyLocalTime, applyLocalTimes, pageFormat} from './local-time.js';

test('the clock and date order of the person', () => {
  const t = '2026-02-03T14:05:00Z';
  expect(formatLocalTime(t, 'daytime', 'UTC', {clock: '12', order: 'dmy'})).toBe('3 Feb 2:05 PM');
  expect(formatLocalTime(t, 'daytime', 'UTC', {clock: '24', order: 'mdy'})).toBe('Feb 3 14:05');
  expect(formatLocalTime(t, 'daytime', 'UTC', {clock: '24', order: 'ymd'})).toBe('2026-02-03 14:05');
  expect(formatLocalTime(t, 'day', 'UTC', {clock: '12', order: 'mdy'})).toBe('Feb 3, 2026');
  expect(formatLocalTime(t, 'day', 'UTC', {clock: '12', order: 'ymd'})).toBe('2026-02-03');
  expect(formatLocalTime('2026-02-03T00:30:00Z', 'daytime', 'UTC', {clock: '12', order: 'dmy'})).toBe('3 Feb 12:30 AM');
  expect(formatLocalTime('2026-02-03T12:00:00Z', 'daytime', 'UTC', {clock: '12', order: 'dmy'})).toBe('3 Feb 12:00 PM');
  expect(formatLocalTime(t, 'daytime', 'Asia/Tokyo', {clock: '12', order: 'mdy'})).toBe('Feb 3 11:05 PM');
  // the default is the board format
  expect(formatLocalTime(t, 'daytime', 'UTC')).toBe('3 Feb 14:05');
});

test('the format on the page; a value it does not know is the default', () => {
  const el = document.createElement('html');
  expect(pageFormat(el)).toEqual({clock: '24', order: 'dmy'});
  el.setAttribute('data-clock', '12');
  el.setAttribute('data-date-order', 'ymd');
  expect(pageFormat(el)).toEqual({clock: '12', order: 'ymd'});
  el.setAttribute('data-clock', '36');
  el.setAttribute('data-date-order', 'ydm');
  expect(pageFormat(el)).toEqual({clock: '24', order: 'dmy'});
});

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
