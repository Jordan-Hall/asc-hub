import {GET} from '../modules/fetch.js';
import {fillMenuStatus, filterMenu, keepMenuScroll, matchSetting} from './asc-settings-find.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

afterEach(() => GET.mockReset());

test('an empty query matches everything', () => {
  expect(matchSetting('Webhooks', '')).toBe(true);
  expect(matchSetting('Webhooks', '   ')).toBe(true);
});

test('the match ignores case and accents', () => {
  expect(matchSetting('Secrets and variables', 'VARIA')).toBe(true);
  expect(matchSetting('Café', 'cafe')).toBe(true);
  expect(matchSetting('Audit log', 'hook')).toBe(false);
});

const menuHtml = `<nav class="ds-settings-nav" id="settings-menu" aria-label="Settings" data-snav-live-url="/o/r/settings/asc/menu-status">
  <div class="ds-settings-bar"><a href="#main-content">Back</a></div>
  <div class="ds-settings-nav-in">
    <input data-asc-snav-find>
    <h2 class="ds-settings-group">Repository</h2>
    <ul class="ds-settings-list">
      <li><a href="/o/r/settings" data-snav-desc="Name, visibility">General</a></li>
      <li><a href="/o/r/settings/branches" data-snav-desc="Protected branches, required checks">Rules</a></li>
    </ul>
    <h2 class="ds-settings-group">Data</h2>
    <ul class="ds-settings-list">
      <li><a href="/o/r/settings/asc/storage" data-snav-desc="Bucket, S3">Storage<span data-snav-live="storage" hidden></span></a></li>
      <li><a href="/o/r/settings/asc/ai" data-snav-desc="Providers">AI keys<span data-snav-live="ai" hidden></span></a></li>
    </ul>
    <a class="ds-snav-danger" href="#danger-zone" data-snav-desc="Transfer, archive, delete">Danger zone</a>
    <p class="ds-hidden" data-asc-snav-none>No setting matches.</p>
  </div>
</nav>`;

const stay = (e) => e.preventDefault();

function mountMenu() {
  document.body.innerHTML = menuHtml;
  return document.querySelector('.ds-settings-nav');
}

test('find matches the description as well as the name, and hides the groups left empty', () => {
  const menu = mountMenu().querySelector('.ds-settings-nav-in');
  expect(filterMenu(menu, 'required checks')).toBe(1);
  const shown = Array.from(menu.querySelectorAll('li:not([hidden]) a'), (a) => a.textContent);
  expect(shown).toEqual(['Rules']);
  expect(menu.querySelectorAll('.ds-settings-group')[1].hidden).toBe(true);
  expect(menu.querySelector('.ds-snav-danger').hidden).toBe(true);

  expect(filterMenu(menu, 'archive')).toBe(1);
  expect(menu.querySelector('.ds-snav-danger').hidden).toBe(false);

  expect(filterMenu(menu, 'nothing like this')).toBe(0);
  expect(menu.querySelector('[data-asc-snav-none]').classList.contains('ds-hidden')).toBe(false);
  expect(filterMenu(menu, '')).toBe(5);
});

test('the menu comes back where it was scrolled when its entry was tapped', () => {
  sessionStorage.clear();
  const nav = mountMenu();
  window.location.hash = '';
  keepMenuScroll(nav);
  nav.scrollTop = 240;
  nav.querySelector('a[href$="/branches"]').addEventListener('click', stay);
  nav.querySelector('a[href$="/branches"]').click();
  expect(sessionStorage.getItem('asc-snav-scroll:Settings')).toBe('240');

  nav.scrollTop = 0;
  nav.querySelector('.ds-settings-bar a').addEventListener('click', stay);
  nav.querySelector('.ds-settings-bar a').click();
  expect(sessionStorage.getItem('asc-snav-scroll:Settings'), 'the back arrow is not an entry').toBe('240');

  window.location.hash = '#settings-menu';
  window.dispatchEvent(new HashChangeEvent('hashchange'));
  expect(nav.scrollTop).toBe(240);
});

test('the live statuses are filled from the menu status JSON, and a missing one stays hidden', async () => {
  const nav = mountMenu();
  GET.mockResolvedValue({ok: true, json: () => Promise.resolve({storage: 'Your bucket'})});
  expect(await fillMenuStatus(nav)).toBe(1);
  expect(GET).toHaveBeenCalledWith('/o/r/settings/asc/menu-status');
  const storage = nav.querySelector('[data-snav-live="storage"]');
  expect(storage.hidden).toBe(false);
  expect(storage.textContent).toBe('Your bucket');
  expect(nav.querySelector('[data-snav-live="ai"]').hidden).toBe(true);
});

test('a failed status load leaves the menu as it was drawn', async () => {
  const nav = mountMenu();
  GET.mockResolvedValue({ok: false});
  expect(await fillMenuStatus(nav)).toBe(0);
  GET.mockRejectedValue(new TypeError('offline'));
  expect(await fillMenuStatus(nav)).toBe(0);
  expect(nav.querySelectorAll('[data-snav-live]:not([hidden])')).toHaveLength(0);
});
