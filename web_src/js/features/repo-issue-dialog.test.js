// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {test, expect, vi, afterEach} from 'vitest';
import {initRepoIssueDialog} from './repo-issue-dialog.js';

function page() {
  document.body.innerHTML = `<a href="/o/r/issues/new" data-issue-dialog="issue-new-dialog">New issue</a>
  <dialog id="issue-new-dialog">
    <form action="/o/r/issues/new" data-issue-dialog-form data-failed="Could not create." data-creating="Creating…">
      <input name="title" value="Crash on start" required><textarea name="content">steps</textarea>
      <p data-issue-dialog-error hidden></p>
      <p data-asc-ncd-note></p>
      <a href="/o/r/issues/new" data-issue-full>Full form</a>
      <button type="submit" data-issue-dialog-submit>Create</button>
    </form>
  </dialog>`;
  const dialog = document.querySelector('dialog');
  dialog.showModal = function () {
    this.open = true;
  };
  dialog.close = function () {
    this.open = false;
    this.dispatchEvent(new Event('close'));
  };
  return dialog;
}

function fakeSaver() {
  return {save: vi.fn(async () => 'saved'), sync: vi.fn()};
}

const flush = () => new Promise((r) => setTimeout(r, 0));

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  document.body.innerHTML = '';
});

test('the button opens the dialog and a closed dialog keeps the draft', () => {
  const dialog = page();
  const saver = fakeSaver();
  expect(initRepoIssueDialog(document, saver)).toBe(dialog);
  const click = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0});
  document.querySelector('[data-issue-dialog]').dispatchEvent(click);
  expect(click.defaultPrevented).toBe(true);
  expect(dialog.open).toBe(true);
  dialog.close();
  expect(saver.save).toHaveBeenCalledTimes(1);

  const newTab = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0, ctrlKey: true});
  document.querySelector('[data-issue-dialog]').dispatchEvent(newTab);
  expect(newTab.defaultPrevented).toBe(false);
});

test('without the dialog the link is left alone', () => {
  document.body.innerHTML = '<a href="/o/r/issues/new">New issue</a>';
  expect(initRepoIssueDialog(document, fakeSaver())).toBeNull();
});

test('a created issue clears the draft state and opens the issue', async () => {
  page();
  const saver = fakeSaver();
  const navigate = vi.fn();
  const fetch = vi.fn(async () => new Response(JSON.stringify({redirect: '/o/r/issues/7'}), {status: 200}));
  vi.stubGlobal('fetch', fetch);
  initRepoIssueDialog(document, saver, {navigate});
  document.querySelector('form').dispatchEvent(new SubmitEvent('submit', {bubbles: true, cancelable: true}));
  await flush();
  await flush();
  expect(saver.save).toHaveBeenCalled();
  expect(fetch).toHaveBeenCalledTimes(1);
  const [url, opts] = fetch.mock.calls[0];
  expect(url).toBe('/o/r/issues/new');
  expect(opts.method).toBe('POST');
  expect(opts.body.get('title')).toBe('Crash on start');
  expect(opts.body.get('content')).toBe('steps');
  expect(saver.sync).toHaveBeenCalled();
  expect(navigate).toHaveBeenCalledWith('/o/r/issues/7');
  // the dialog closing as the page goes does not save the draft again
  saver.save.mockClear();
  document.querySelector('dialog').close();
  expect(saver.save).not.toHaveBeenCalled();
});

test('a refused issue shows the error in the dialog and keeps the text', async () => {
  page();
  const navigate = vi.fn();
  vi.stubGlobal('fetch', vi.fn(async () => new Response(JSON.stringify({errorMessage: 'Title is empty.'}), {status: 400})));
  initRepoIssueDialog(document, fakeSaver(), {navigate});
  document.querySelector('form').dispatchEvent(new SubmitEvent('submit', {bubbles: true, cancelable: true}));
  await flush();
  await flush();
  const error = document.querySelector('[data-issue-dialog-error]');
  expect(error.hidden).toBe(false);
  expect(error.textContent).toBe('Title is empty.');
  expect(document.querySelector('[data-issue-dialog-submit]').disabled).toBe(false);
  expect(document.querySelector('[name=title]').value).toBe('Crash on start');
  expect(navigate).not.toHaveBeenCalled();

  vi.stubGlobal('fetch', vi.fn(async () => {
    throw new TypeError('offline');
  }));
  document.querySelector('form').dispatchEvent(new SubmitEvent('submit', {bubbles: true, cancelable: true}));
  await flush();
  await flush();
  expect(error.textContent).toBe('Could not create.');
});

test('the full form saves the draft before it opens', async () => {
  page();
  const saver = fakeSaver();
  const navigate = vi.fn();
  initRepoIssueDialog(document, saver, {navigate});
  const click = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0});
  document.querySelector('[data-issue-full]').dispatchEvent(click);
  expect(click.defaultPrevented).toBe(true);
  await flush();
  expect(saver.save).toHaveBeenCalledTimes(1);
  expect(navigate).toHaveBeenCalledWith('/o/r/issues/new');
});
