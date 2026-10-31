import {test, expect, vi, beforeEach, afterEach} from 'vitest';
import {createSaver, draftSignature, initAscNewChangeDraft, AUTOSAVE_DELAY_MS} from './asc-newchange-draft.js';

function page() {
  document.body.innerHTML = `<form data-asc-ncd-url="/o/r/asc/newchange_draft" data-asc-ncd-id="${'a'.repeat(24)}">
    <input name="title" value=""><textarea name="content"></textarea>
    <span data-asc-ncd-note data-saved="Saved." data-failed="Failed." data-discarded="Gone."></span>
    <button type="button" data-asc-ncd-save>Save</button>
    <button type="button" data-asc-ncd-discard data-confirm="Sure?">Discard</button>
  </form>`;
  return document.querySelector('form');
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

test('the signature changes with either field', () => {
  const form = page();
  const a = draftSignature(form);
  form.querySelector('[name=title]').value = 'x';
  const b = draftSignature(form);
  form.querySelector('[name=content]').value = 'y';
  expect(new Set([a, b, draftSignature(form)]).size).toBe(3);
});

test('the saver sends changed text once and not unchanged text', async () => {
  const form = page();
  const send = vi.fn(async () => true);
  const saver = createSaver(form, send);
  expect(await saver.save()).toBe('unchanged');
  form.querySelector('[name=title]').value = 'Add retries';
  form.querySelector('[name=content]').value = '## What';
  expect(await saver.save()).toBe('saved');
  expect(await saver.save()).toBe('unchanged');
  expect(send).toHaveBeenCalledTimes(1);
  const data = send.mock.calls[0][0];
  expect([data.get('id'), data.get('title'), data.get('content')]).toEqual(['a'.repeat(24), 'Add retries', '## What']);
  expect(await saver.save(true)).toBe('saved');
  expect(send).toHaveBeenCalledTimes(2);
});

test('a failed save is reported and tried again at the next change', async () => {
  const form = page();
  const send = vi.fn().mockResolvedValueOnce(false).mockRejectedValueOnce(new Error('offline')).mockResolvedValue(true);
  const saver = createSaver(form, send);
  form.querySelector('[name=title]').value = 'x';
  expect(await saver.save()).toBe('failed');
  expect(await saver.save()).toBe('failed');
  expect(await saver.save()).toBe('saved');
});

test('saves run one after the other', async () => {
  const form = page();
  const order = [];
  const send = vi.fn(async (data) => {
    order.push(`start ${data.get('title')}`);
    await Promise.resolve();
    order.push(`end ${data.get('title')}`);
    return true;
  });
  const saver = createSaver(form, send);
  const title = form.querySelector('[name=title]');
  title.value = 'one';
  const first = saver.save();
  title.value = 'two';
  const second = saver.save();
  await Promise.all([first, second]);
  expect(order).toEqual(['start two', 'end two']);
  // the first call already read the text when it ran, which was after the second edit: one request for the latest text
  expect(send).toHaveBeenCalledTimes(1);
});

test('leaving a field saves after a second, and a second blur restarts the wait', async () => {
  const form = page();
  const fetchMock = vi.fn(async () => ({ok: true}));
  vi.stubGlobal('fetch', fetchMock);
  initAscNewChangeDraft(document);
  const title = form.querySelector('[name=title]');
  title.value = 'Add retries';
  title.dispatchEvent(new Event('blur'));
  await vi.advanceTimersByTimeAsync(AUTOSAVE_DELAY_MS - 100);
  form.querySelector('[name=content]').dispatchEvent(new Event('blur'));
  await vi.advanceTimersByTimeAsync(AUTOSAVE_DELAY_MS - 100);
  expect(fetchMock).not.toHaveBeenCalled();
  await vi.advanceTimersByTimeAsync(200);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(fetchMock.mock.calls[0][0]).toBe('/o/r/asc/newchange_draft');
  expect(fetchMock.mock.calls[0][1].method).toBe('POST');
  expect(form.querySelector('[data-asc-ncd-note]').textContent).toBe('Saved.');
});

test('Save as draft saves at once and says when it fails', async () => {
  const form = page();
  const fetchMock = vi.fn(async () => ({ok: false}));
  vi.stubGlobal('fetch', fetchMock);
  initAscNewChangeDraft(document);
  form.querySelector('[name=title]').value = 'x';
  form.querySelector('[data-asc-ncd-save]').click();
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock).toHaveBeenCalledTimes(1);
  expect(form.querySelector('[data-asc-ncd-note]').textContent).toBe('Failed.');
});

test('Discard asks, removes the draft and clears the form', async () => {
  const form = page();
  const fetchMock = vi.fn(async () => ({ok: true}));
  vi.stubGlobal('fetch', fetchMock);
  const confirm = vi.fn();
  vi.stubGlobal('confirm', confirm);
  window.confirm = confirm;
  initAscNewChangeDraft(document);
  form.querySelector('[name=title]').value = 'keep me';

  confirm.mockReturnValueOnce(false);
  form.querySelector('[data-asc-ncd-discard]').click();
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock).not.toHaveBeenCalled();
  expect(form.querySelector('[name=title]').value).toBe('keep me');

  confirm.mockReturnValueOnce(true);
  form.querySelector('[data-asc-ncd-discard]').click();
  await vi.advanceTimersByTimeAsync(0);
  expect(fetchMock.mock.calls[0][1].body.get('discard')).toBe('1');
  expect(form.querySelector('[name=title]').value).toBe('');
  expect(form.querySelector('[data-asc-ncd-note]').textContent).toBe('Gone.');
});

test('a page without the draft form does nothing', () => {
  document.body.innerHTML = '<form><input name="title"></form>';
  expect(initAscNewChangeDraft(document)).toBeNull();
});
