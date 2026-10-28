import {initPrefs, readPref, shortcutsEnabled} from './ds-prefs.js';

beforeEach(() => {
  localStorage.clear();
  document.body.innerHTML = '';
  document.documentElement.removeAttribute('data-pref-motion');
});

test('a missing or broken value gives the default', () => {
  expect(readPref('motion')).toBe('0');
  expect(shortcutsEnabled()).toBe(true);
  localStorage.setItem('asc-pref-keys', 'x');
  expect(shortcutsEnabled()).toBe(true);
  localStorage.setItem('asc-pref-keys', '0');
  expect(shortcutsEnabled()).toBe(false);
});

test('a switch applies at once, announces it and Undo puts it back', () => {
  document.body.innerHTML = '<input type="checkbox" data-pref="motion" data-toast-on="Motion reduced" data-toast-off="Motion restored" data-toast-undo="Undo">';
  initPrefs();
  const box = document.querySelector('input');
  expect(box.checked).toBe(false);
  box.checked = true;
  box.dispatchEvent(new Event('change'));
  expect(localStorage.getItem('asc-pref-motion')).toBe('1');
  expect(document.documentElement.getAttribute('data-pref-motion')).toBe('on');
  expect(document.querySelector('.ds-toast-text').textContent).toBe('Motion reduced');
  document.querySelector('.ds-toast-action').click();
  expect(box.checked).toBe(false);
  expect(localStorage.getItem('asc-pref-motion')).toBe('0');
  expect(document.documentElement.getAttribute('data-pref-motion')).toBe('off');
});
