import {formatUserCode, initAscDevice} from './asc-device.js';

test('the code is shown in capitals with a hyphen after four characters', () => {
  expect(formatUserCode('wdjb')).toBe('WDJB');
  expect(formatUserCode('wdjbm')).toBe('WDJB-M');
  expect(formatUserCode('wdjb-mjht')).toBe('WDJB-MJHT');
  expect(formatUserCode(' wdjb mjht ')).toBe('WDJB-MJHT');
  expect(formatUserCode('wdjbmjhtxyz')).toBe('WDJB-MJHT');
  expect(formatUserCode('')).toBe('');
});

test('typing into the field formats it', () => {
  document.body.innerHTML = '<form action="/login/device"><input id="user_code"></form>';
  initAscDevice();
  const input = document.querySelector('#user_code');
  input.value = 'abcdefg';
  input.dispatchEvent(new Event('input'));
  expect(input.value).toBe('ABCD-EFG');
  expect(input.getAttribute('autocapitalize')).toBe('characters');
});
