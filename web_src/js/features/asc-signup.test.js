import {emailLooksValid, handleState} from './asc-signup.js';

test('a name is empty, typing or invalid', () => {
  expect(handleState('')).toBe('empty');
  expect(handleState('jordan.h-1_x')).toBe('typing');
  expect(handleState('two words')).toBe('invalid');
  expect(handleState('a'.repeat(41))).toBe('invalid');
  expect(handleState('<b>')).toBe('invalid');
});

test('an address looks valid or not', () => {
  expect(emailLooksValid('maya@acme.dev')).toBe(true);
  expect(emailLooksValid('maya.o+tag@mail.acme.co.uk')).toBe(true);
  expect(emailLooksValid('maya')).toBe(false);
  expect(emailLooksValid('maya@acme')).toBe(false);
  expect(emailLooksValid('maya@@acme.dev')).toBe(false);
  expect(emailLooksValid('may a@acme.dev')).toBe(false);
  expect(emailLooksValid('maya@acme..dev')).toBe(false);
});
