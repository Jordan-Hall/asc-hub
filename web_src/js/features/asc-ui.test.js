import {archFromHints, osFromPlatform, initAscInstallPills} from './asc-ui.js';

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

test('install pills press one pill and show only its command', () => {
  document.body.innerHTML = `<div data-ds-install>
    <button class="ds-install-pill" data-install-key="brew" aria-pressed="true"></button>
    <button class="ds-install-pill" data-install-key="scoop" aria-pressed="false"></button>
    <div class="ds-cmd" data-install-key="brew"></div><div class="ds-cmd" data-install-key="scoop" hidden></div></div>`;
  initAscInstallPills();
  document.querySelector('[data-install-key="scoop"].ds-install-pill').click();
  expect(Array.from(document.querySelectorAll('.ds-install-pill'), (p) => p.getAttribute('aria-pressed'))).toEqual(['false', 'true']);
  expect(Array.from(document.querySelectorAll('.ds-cmd'), (c) => c.hidden)).toEqual([true, false]);
});
