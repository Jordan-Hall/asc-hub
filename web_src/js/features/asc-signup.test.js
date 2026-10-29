import {handleState} from './asc-signup.js';

test('a name is empty, typing or invalid', () => {
  expect(handleState('')).toBe('empty');
  expect(handleState('jordan.h-1_x')).toBe('typing');
  expect(handleState('two words')).toBe('invalid');
  expect(handleState('a'.repeat(41))).toBe('invalid');
  expect(handleState('<b>')).toBe('invalid');
});
