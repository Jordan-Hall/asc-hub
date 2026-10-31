import {matchSetting} from './asc-settings-find.js';

test('an empty query matches everything', () => {
  expect(matchSetting('Webhooks', '')).toBe(true);
  expect(matchSetting('Webhooks', '   ')).toBe(true);
});

test('the match ignores case and accents', () => {
  expect(matchSetting('Secrets and variables', 'VARIA')).toBe(true);
  expect(matchSetting('Café', 'cafe')).toBe(true);
  expect(matchSetting('Audit log', 'hook')).toBe(false);
});
