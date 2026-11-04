import {test, expect, vi} from 'vitest';
import {applyDraft, applySummary, askSummary, newOpID} from './asc-draft.js';

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

const brief = (values = {}) => Object.fromEntries(['what', 'why', 'how', 'risk_note', 'where'].map((n) => [n, field(values[n] || '')]));

test('a summary fills every field of the brief', () => {
  const fields = brief();
  const summary = {what: 'Adds sub', why: 'Asked', how: 'cargo test', risk: 'Low', where: 'src/lib.rs:4 the function\n'};
  expect(applySummary(summary, fields, () => false)).toBe('applied');
  expect(fields.what.value).toBe('Adds sub');
  expect(fields.risk_note.value).toBe('Low');
  expect(fields.where.value).toBe('src/lib.rs:4 the function\n');
  expect(fields.how.dispatchEvent).toHaveBeenCalled();
});

test('a brief the person wrote is kept unless they agree', () => {
  const fields = brief({where: 'mine.rs'});
  expect(applySummary({what: 'x'}, fields, () => false)).toBe('kept');
  expect(fields.where.value).toBe('mine.rs');
  expect(applySummary({what: 'x'}, fields, () => true)).toBe('applied');
  expect(fields.where.value).toBe('');
  expect(applySummary({nothing: true}, brief(), () => true)).toBe('nothing');
});

test('an op id is a UUID version 7', () => {
  const id = newOpID();
  expect(id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
  expect(newOpID()).not.toBe(id);
});

test('askSummary keeps the next op id and reads a refusal', async () => {
  const btn = document.createElement('button');
  btn.setAttribute('data-summary-url', '/o/r/asc/summarize');
  const calls = [];
  vi.stubGlobal('fetch', vi.fn(async (url, opts) => {
    calls.push({url, opts});
    return calls.length === 1 ?
      new Response(JSON.stringify({error: 'no_key', message: 'No AI key.', next: 'n1'}), {status: 409}) :
      new Response(JSON.stringify({what: 'Adds sub', body: '### What', next: 'n2'}), {status: 200});
  }));
  const first = await askSummary(btn, new URLSearchParams());
  expect(first.refusal).toEqual({error: 'no_key', message: 'No AI key.'});
  expect(calls[0].opts.body.get('op')).toMatch(/^[0-9a-f-]{36}$/);
  expect(btn.getAttribute('data-op')).toBe('n1');
  const second = await askSummary(btn, new URLSearchParams());
  expect(second.summary.what).toBe('Adds sub');
  expect(calls[1].opts.body.get('op')).toBe('n1');
  expect(btn.getAttribute('data-op')).toBe('n2');
  vi.stubGlobal('fetch', vi.fn(async () => {
    throw new Error('offline');
  }));
  expect((await askSummary(btn, new URLSearchParams())).refusal.error).toBe('unavailable');
  vi.unstubAllGlobals();
});
