import {stripShellPrompt} from './codecopy.js';

test('a block of commands is copied without the prompt', () => {
  expect(stripShellPrompt('$ asc sync\n$ asc undo')).toBe('asc sync\nasc undo');
  expect(stripShellPrompt('$ asc sync\n\n$ asc undo')).toBe('asc sync\n\nasc undo');
});

test('a block that mixes commands and output is copied as it is', () => {
  const mixed = '$ asc --version\nasc 1.4.2';
  expect(stripShellPrompt(mixed)).toBe(mixed);
});

test('a block without a prompt, and a dollar inside a line, are left alone', () => {
  expect(stripShellPrompt('asc sync')).toBe('asc sync');
  expect(stripShellPrompt('echo "$ x"')).toBe('echo "$ x"');
  expect(stripShellPrompt('')).toBe('');
});
