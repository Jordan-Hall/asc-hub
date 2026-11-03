import {hideSettingsLoading, initSettingsLoading, loadsOtherPage, showSettingsLoading} from './ds-settings-loading.js';

const stop = (e) => e.preventDefault();
const here = new URL('https://hub.example.org/user/settings');
const click = (extra = {}) => ({button: 0, defaultPrevented: false, ...extra});
const link = (href, attrs = {}) => {
  const a = document.createElement('a');
  a.href = href;
  for (const [k, v] of Object.entries(attrs)) a.setAttribute(k, v);
  return a;
};

test('a plain click on another settings page loads it; the same page, a new tab or another site does not', () => {
  expect(loadsOtherPage(link('https://hub.example.org/user/settings/account'), click(), here)).toBe(true);
  expect(loadsOtherPage(link('https://hub.example.org/user/settings'), click(), here)).toBe(false);
  expect(loadsOtherPage(link('https://hub.example.org/user/settings#x'), click(), here)).toBe(false);
  expect(loadsOtherPage(link('https://hub.example.org/user/settings/account'), click({ctrlKey: true}), here)).toBe(false);
  expect(loadsOtherPage(link('https://hub.example.org/user/settings/account'), click({button: 1}), here)).toBe(false);
  expect(loadsOtherPage(link('https://hub.example.org/user/settings/account', {target: '_blank'}), click(), here)).toBe(false);
  expect(loadsOtherPage(link('https://other.example.org/user/settings/account'), click(), here)).toBe(false);
});

test('the content gives way to skeleton inputs that screen readers hear as loading, and comes back', () => {
  document.body.innerHTML = '<div class="ds-settings-body" data-loading="Loading…"><h1>Profile</h1><form></form></div>';
  const body = document.querySelector('.ds-settings-body');
  showSettingsLoading(body);
  showSettingsLoading(body);
  expect(body.querySelectorAll('.ds-settings-sk')).toHaveLength(1);
  expect(body.querySelectorAll('.ds-settings-sk-input')).toHaveLength(4);
  expect(body.getAttribute('aria-busy')).toBe('true');
  expect(body.querySelector('[role="status"]').textContent).toBe('Loading…');
  expect(body.querySelectorAll('.ds-settings-sk-field[aria-hidden="true"]')).toHaveLength(4);

  hideSettingsLoading(body);
  expect(body.querySelector('.ds-settings-sk')).toBeNull();
  expect(body.hasAttribute('aria-busy')).toBe(false);
  expect(body.querySelector('h1').textContent).toBe('Profile');
});

test('a click on a link of the menu shows the skeleton', () => {
  document.body.innerHTML = `
    <nav class="ds-settings-nav"><a id="to" href="/user/settings/account">Account</a><a id="here" href="${window.location.pathname}">Here</a></nav>
    <div class="ds-settings-body" data-loading="Loading…"><h1>Profile</h1></div>`;
  initSettingsLoading();
  const body = document.querySelector('.ds-settings-body');
  document.addEventListener('click', stop);
  document.querySelector('#here').click();
  expect(body.classList.contains('is-loading')).toBe(false);
  document.removeEventListener('click', stop);

  const nav = document.querySelector('.ds-settings-nav');
  nav.addEventListener('click', stop);
  // the default action is stopped after the menu saw the click, so the test page stays
  document.querySelector('#to').dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, button: 0}));
  expect(body.classList.contains('is-loading')).toBe(true);
});
