import {checkPassword, classesOf} from './asc-password.js';

test('classesOf', () => {
  expect(classesOf('')).toEqual([]);
  expect(classesOf('aB3!')).toEqual(['lower', 'upper', 'digit', 'spec']);
  expect(classesOf('abc def')).toEqual(['lower', 'spec']);
});

test('checkPassword levels', () => {
  expect(checkPassword('').level).toBe(0);
  expect(checkPassword('short', {min: 8}).level).toBe(1);
  expect(checkPassword('aaaaaaaaaaaa', {min: 8}).level).toBe(1);
  expect(checkPassword('longenough', {min: 8}).level).toBe(2);
  expect(checkPassword('Longer-pass1', {min: 8}).level).toBe(3);
  expect(checkPassword('Correct-horse-battery9', {min: 8}).level).toBe(4);
  expect(checkPassword(`${'a'.repeat(12)}bcdefghijklmnopqrstuvwx`, {min: 8}).level).toBe(4);
});

test('required classes keep the level at weak until they are present', () => {
  const required = ['lower', 'upper', 'digit'];
  const lower = checkPassword('onlylowercase', {min: 8, required});
  expect(lower.level).toBe(1);
  expect(lower.rules).toMatchObject({length: true, lower: true, upper: false, digit: false});
  expect(checkPassword('Onlylower1', {min: 8, required}).level).toBe(2);
});
