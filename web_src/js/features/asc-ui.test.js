import {archFromHints, osFromPlatform} from './asc-ui.js';

test('archFromHints names only the builds asc ships', () => {
  expect(archFromHints({architecture: 'arm', bitness: '64'})).toBe('arm64');
  expect(archFromHints({architecture: 'x86', bitness: '64'})).toBe('x86_64');
  expect(archFromHints({architecture: 'riscv'})).toBeNull();
  expect(archFromHints({})).toBeNull();
  expect(archFromHints(null)).toBeNull();
  expect(archFromHints({architecture: 5})).toBeNull();
});

test('osFromPlatform maps the client hint', () => {
  expect(osFromPlatform('Windows')).toBe('windows');
  expect(osFromPlatform('macOS')).toBe('macos');
  expect(osFromPlatform('Linux')).toBe('linux');
  expect(osFromPlatform('Android')).toBeNull();
  expect(osFromPlatform(undefined)).toBeNull();
});
