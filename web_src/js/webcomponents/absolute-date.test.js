import {toAbsoluteLocaleDate, toOrderedDate} from './absolute-date.js';

test('toOrderedDate follows the date order of the person', () => {
  const opts = {year: 'numeric', month: 'short', day: 'numeric'};
  expect(toOrderedDate('2026-02-03', 'de-DE', opts, 'dmy')).toEqual('3 Feb 2026');
  expect(toOrderedDate('2026-02-03', 'de-DE', opts, 'mdy')).toEqual('Feb 3, 2026');
  expect(toOrderedDate('2026-02-03T10:00:00', 'de-DE', opts, 'ymd')).toEqual('2026-02-03');
  expect(toOrderedDate('2026-02-03', 'en-US', {...opts, weekday: 'short'}, 'ymd')).toEqual('Tue 2026-02-03');
  expect(toOrderedDate('2026-02-03', 'de-DE', opts, 'unknown')).toEqual('3. Feb. 2026');
});

test('toAbsoluteLocaleDate', () => {
  expect(toAbsoluteLocaleDate('2024-03-15', 'en-US', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })).toEqual('March 15, 2024');

  expect(toAbsoluteLocaleDate('2024-03-15T01:02:03', 'de-DE', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })).toEqual('15. März 2024');

  // these cases shouldn't happen
  expect(toAbsoluteLocaleDate('2024-03-15 01:02:03', '', {})).toEqual('Invalid Date');
  expect(toAbsoluteLocaleDate('10000-01-01', '', {})).toEqual('Invalid Date');

  // test different timezone
  vi.stubEnv('TZ', 'America/New_York');
  expect(new Date('2024-03-15').toLocaleString('en-US')).toEqual('3/14/2024, 8:00:00 PM');
  expect(toAbsoluteLocaleDate('2024-03-15', 'en-US')).toEqual('3/15/2024, 12:00:00 AM');
});

test('absolute-date structure', () => {
  const element = document.createElement('absolute-date');
  element.setAttribute('date', '2026-06-16T00:00:00Z');
  element.setAttribute('year', 'numeric');

  document.body.append(element);

  const shadowRoot = element.shadowRoot;
  const childSpan = shadowRoot.querySelector('span');

  expect(shadowRoot).toBeTruthy(); // verifies if isolated open shadow root exists
  expect(childSpan).toBeTruthy(); // verifies that a clean <span> tag was spawned
  expect(childSpan.getAttribute('part')).toBe('absolute-date'); // verifies the CSS styling hook bridge
  expect(childSpan.textContent).toContain('2026'); // verifies that the date string outputs

  element.remove();// clean up DOM env
});
