import {reconnectText} from './asc-agent-run.ts';

test('the reconnect line names the time of the last answer', () => {
  const at = new Date(2026, 10, 4, 10, 14, 30);
  expect(reconnectText('Showing the log as of {time}. It catches up on its own.', at, 'en-GB')).toBe('Showing the log as of 10:14. It catches up on its own.');
  expect(reconnectText('{time}', at, 'en-US')).toMatch(/^10:14\sAM$/);
  expect(reconnectText('no time here', at, 'en-GB')).toBe('no time here');
});
