import {test, expect, vi} from 'vitest';
import {applyDraft} from './asc-draft.js';

const field = (value = '') => ({value, dispatchEvent: vi.fn()});

test('an empty form gets the draft', () => {
  const title = field();
  const body = field();
  expect(applyDraft({title: 'Add retry', body: '### What'}, {title, body}, () => false)).toBe('applied');
  expect(title.value).toBe('Add retry');
  expect(body.value).toBe('### What');
});

test('what the person wrote is kept unless they agree', () => {
  const title = field('Mine');
  const body = field();
  expect(applyDraft({title: 'Add retry', body: 'x'}, {title, body}, () => false)).toBe('kept');
  expect(title.value).toBe('Mine');
  expect(applyDraft({title: 'Add retry', body: 'x'}, {title, body}, () => true)).toBe('applied');
  expect(title.value).toBe('Add retry');
});

test('nothing to compare changes nothing', () => {
  const title = field();
  const body = field();
  expect(applyDraft({nothing: true}, {title, body}, () => true)).toBe('nothing');
  expect(title.value).toBe('');
});
