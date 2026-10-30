import {initRepoSettingsTopics, announceAccessChange} from './repo-settings.js';

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
