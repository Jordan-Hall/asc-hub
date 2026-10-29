import {draftKey, packDraft, unpackDraft, initAscDrafts} from './asc-drafts.js';

function memory() {
  const m = new Map();
  return {getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, v), removeItem: (k) => m.delete(k), m};
}

function page(initial = '') {
  document.body.innerHTML = `<form><div class="combo-markdown-editor"><textarea name="content">${initial}</textarea></div></form>`;
  return document.querySelector('textarea');
}

test('a draft is packed with its time and expires after 30 days', () => {
  expect(unpackDraft(packDraft('hello', 1000), 2000)).toBe('hello');
  expect(unpackDraft(packDraft('hello', 0), 31 * 24 * 3600 * 1000)).toBe('');
  expect(unpackDraft('not json')).toBe('');
  expect(unpackDraft(null)).toBe('');
  expect(unpackDraft(JSON.stringify({t: 5, at: 1}))).toBe('');
  expect(JSON.parse(packDraft('x'.repeat(30000))).t).toHaveLength(20 * 1024);
});

test('an empty field gets its draft back and the draft follows the typing', () => {
  vi.useFakeTimers();
  const store = memory();
  store.setItem(draftKey('/a/b/issues/3', 'content'), packDraft('half a thought'));
  const area = page();
  initAscDrafts(document, store, '/a/b/issues/3');
  expect(area.value).toBe('half a thought');

  area.value = 'a longer thought';
  area.dispatchEvent(new Event('input'));
  vi.advanceTimersByTime(1100);
  expect(unpackDraft(store.getItem(draftKey('/a/b/issues/3', 'content')))).toBe('a longer thought');

  // emptied: nothing is kept
  area.value = '  ';
  area.dispatchEvent(new Event('input'));
  vi.advanceTimersByTime(1100);
  expect(store.getItem(draftKey('/a/b/issues/3', 'content'))).toBeNull();
  vi.useRealTimers();
});

test('sending the form drops the draft, and a field that opens with text is left alone', () => {
  vi.useFakeTimers();
  const store = memory();
  const area = page();
  initAscDrafts(document, store, '/p');
  area.value = 'text';
  area.dispatchEvent(new Event('input'));
  vi.advanceTimersByTime(1100);
  expect(store.m.size).toBe(1);
  area.form.dispatchEvent(new Event('submit'));
  expect(store.m.size).toBe(0);

  const edit = page('an existing comment');
  store.setItem(draftKey('/p', 'content'), packDraft('old draft'));
  initAscDrafts(document, store, '/p');
  expect(edit.value).toBe('an existing comment');
  vi.useRealTimers();
});

test('blocked storage costs only the draft', () => {
  const blocked = {getItem() { throw new Error('no') }, setItem() { throw new Error('no') }, removeItem() { throw new Error('no') }};
  const area = page();
  expect(() => initAscDrafts(document, blocked, '/p')).not.toThrow();
  area.dispatchEvent(new Event('input'));
});
