import {initRepoSettingsTopics} from './repo-settings.js';

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
