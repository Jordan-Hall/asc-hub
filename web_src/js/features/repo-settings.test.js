import {initRepoSettingsTopics, announceAccessChange, initVisibilityConfirm} from './repo-settings.js';

vi.mock('../modules/fetch.js', () => ({POST: vi.fn()}));
vi.mock('./codemirror.ts', () => ({createCodemirror: vi.fn()}));

import {POST} from '../modules/fetch.js';

const page = () => {
  document.body.innerHTML = `<form data-topics-form action="/o/r/topics" data-topics-saved="Saved." data-topics-failed="No: %s">
    <input name="topics" value="rust, payments"><span data-topics-status></span></form>`;
  initRepoSettingsTopics();
  return document.querySelector('form');
};

test('the topics form posts the list and says it was saved', async () => {
  POST.mockResolvedValue({ok: true});
  const form = page();
  form.dispatchEvent(new Event('submit', {cancelable: true}));
  await vi.waitFor(() => expect(document.querySelector('[data-topics-status]').textContent).toBe('Saved.'));
  expect(POST).toHaveBeenCalledWith('/o/r/topics', expect.anything());
});

test('the topics form says why a list was refused', async () => {
  POST.mockResolvedValue({ok: false, status: 422, json: async () => ({message: 'too many'})});
  const form = page();
  form.dispatchEvent(new Event('submit', {cancelable: true}));
  await vi.waitFor(() => expect(document.querySelector('[data-topics-status]').textContent).toBe('No: too many'));
});

test('a changed access is announced with an Undo that applies the old role', () => {
  document.body.innerHTML = '<div id="d" data-changed="Access of rev changed to %s." data-undo="Undo" data-close="Close"></div>';
  const undo = vi.fn();
  announceAccessChange(document.getElementById('d'), 'Write', undo);
  const toast = document.querySelector('.ds-toast');
  expect(toast.textContent).toContain('Access of rev changed to Write.');
  toast.querySelector('.ds-toast-action').click();
  expect(undo).toHaveBeenCalledTimes(1);
  expect(document.querySelector('.ds-toast')).toBeNull();
  // without a message nothing is shown
  expect(announceAccessChange(document.createElement('div'), 'Read', undo)).toBeNull();
});

test('changing the visibility asks first and submits only after the answer', () => {
  document.body.innerHTML = `<form data-visibility-form>
    <input type="radio" name="visibility" value="private" checked data-confirm-title="Make it private?" data-confirm-body="Only you.">
    <input type="radio" name="visibility" value="public" data-confirm-title="Make it public?" data-confirm-body="Anyone.">
    <dialog data-visibility-dialog><header data-visibility-title></header><p data-visibility-body></p>
      <button type="button" data-visibility-cancel></button><button type="button" data-visibility-confirm></button></dialog></form>`;
  const form = document.querySelector('form');
  const dialog = form.querySelector('dialog');
  dialog.showModal = vi.fn();
  dialog.close = vi.fn();
  form.requestSubmit = vi.fn(() => form.dispatchEvent(new Event('submit', {cancelable: true})));
  initVisibilityConfirm();
  // unchanged: no question
  let ev = new Event('submit', {cancelable: true});
  form.dispatchEvent(ev);
  expect(ev.defaultPrevented).toBe(false);
  // changed: the dialog opens and the submit waits
  form.querySelector('input[value=public]').checked = true;
  ev = new Event('submit', {cancelable: true});
  form.dispatchEvent(ev);
  expect(ev.defaultPrevented).toBe(true);
  expect(dialog.showModal).toHaveBeenCalled();
  expect(dialog.querySelector('[data-visibility-title]').textContent).toBe('Make it public?');
  // cancel: nothing is sent
  dialog.querySelector('[data-visibility-cancel]').click();
  expect(form.requestSubmit).not.toHaveBeenCalled();
  // confirm: the form is submitted
  form.dispatchEvent(new Event('submit', {cancelable: true}));
  dialog.querySelector('[data-visibility-confirm]').click();
  expect(form.requestSubmit).toHaveBeenCalledTimes(1);
});
