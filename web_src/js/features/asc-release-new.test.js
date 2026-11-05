import {GET} from '../modules/fetch.js';
import {initAscReleaseNew, proposedText, withNotes, writeLabel} from './asc-release-new.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

function page() {
  document.body.innerHTML = `<a href="/o/r/releases/new" data-rel-new-open>New release</a>
  <dialog data-rel-new data-draft-url="/o/r/releases/asc_draft"><form>
    <input name="tag_name"><p data-rel-new-proposed data-text="asc proposes {tag} (latest release {latest})." data-text-first="asc proposes {tag} for the first release." hidden></p>
    <input name="title">
    <button type="button" data-rel-new-write data-label-one="Write from the landed change" data-label-other="Write from the %d landed changes" hidden></button>
    <textarea name="content"></textarea><p data-rel-new-state data-failed="failed" data-truncated="first only"></p>
    <button type="button" data-rel-new-close>Cancel</button>
  </form></dialog>`;
  const dialog = document.querySelector('dialog');
  dialog.showModal = vi.fn(() => dialog.setAttribute('open', ''));
  dialog.close = vi.fn(() => {
    dialog.removeAttribute('open');
    dialog.dispatchEvent(new Event('close'));
  });
  initAscReleaseNew(document);
  return dialog;
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

test('the helpers word the proposal and append the notes', () => {
  const hint = document.createElement('p');
  hint.setAttribute('data-text', 'p {tag} after {latest}');
  hint.setAttribute('data-text-first', 'first {tag}');
  expect(proposedText(hint, {proposed_tag: 'v2.19.0', latest_tag: 'v2.18.0'})).toBe('p v2.19.0 after v2.18.0');
  expect(proposedText(hint, {proposed_tag: 'v1.0.0', latest_tag: ''})).toBe('first v1.0.0');
  expect(proposedText(hint, {proposed_tag: ''})).toBe('');
  const b = document.createElement('button');
  b.setAttribute('data-label-one', 'one');
  b.setAttribute('data-label-other', '%d changes');
  expect(writeLabel(b, 1)).toBe('one');
  expect(writeLabel(b, 14)).toBe('14 changes');
  expect(withNotes('', '## Fixes')).toBe('## Fixes');
  expect(withNotes('Intro\n', '## Fixes')).toBe('Intro\n\n## Fixes');
});

test('the button opens the dialog, proposes the tag and writes the notes', async () => {
  GET.mockResolvedValue({ok: true, status: 200, json: async () => ({latest_tag: 'v2.18.0', proposed_tag: 'v2.19.0', changes: 14, notes: '## Fixes\n- retry', truncated: true})});
  const dialog = page();
  const open = document.querySelector('[data-rel-new-open]');
  const click = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0});
  open.dispatchEvent(click);
  expect(click.defaultPrevented).toBe(true);
  expect(dialog.showModal).toHaveBeenCalled();
  await flush();
  expect(GET).toHaveBeenCalledWith('/o/r/releases/asc_draft');
  expect(dialog.querySelector('[name="tag_name"]').value).toBe('v2.19.0');
  expect(dialog.querySelector('[name="title"]').value).toBe('v2.19.0');
  const hint = dialog.querySelector('[data-rel-new-proposed]');
  expect(hint.hidden).toBe(false);
  expect(hint.textContent).toBe('asc proposes v2.19.0 (latest release v2.18.0).');
  const write = dialog.querySelector('[data-rel-new-write]');
  expect(write.hidden).toBe(false);
  expect(write.textContent).toBe('Write from the 14 landed changes');
  write.click();
  expect(dialog.querySelector('[name="content"]').value).toBe('## Fixes\n- retry');
  expect(dialog.querySelector('[data-rel-new-state]').textContent).toBe('first only');
  dialog.querySelector('[data-rel-new-close]').click();
  expect(document.activeElement).toBe(open);
  open.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, button: 0}));
  await flush();
  expect(GET).toHaveBeenCalledTimes(1);
});

test('the title follows the tag until it is edited', () => {
  GET.mockResolvedValue({ok: false, status: 404});
  const dialog = page();
  const tag = dialog.querySelector('[name="tag_name"]');
  const title = dialog.querySelector('[name="title"]');
  tag.value = 'v3';
  tag.dispatchEvent(new Event('input'));
  expect(title.value).toBe('v3');
  title.value = 'Big one';
  title.dispatchEvent(new Event('input'));
  tag.value = 'v3.0.0';
  tag.dispatchEvent(new Event('input'));
  expect(title.value).toBe('Big one');
});

test('a repository without asc keeps the plain form; an outage says so', async () => {
  GET.mockReset();
  GET.mockResolvedValue({ok: false, status: 404});
  let dialog = page();
  document.querySelector('[data-rel-new-open]').click();
  await flush();
  expect(dialog.querySelector('[data-rel-new-write]').hidden).toBe(true);
  expect(dialog.querySelector('[data-rel-new-state]').textContent).toBe('');
  GET.mockResolvedValue({ok: false, status: 503});
  dialog = page();
  document.querySelector('[data-rel-new-open]').click();
  await flush();
  expect(dialog.querySelector('[data-rel-new-state]').textContent).toBe('failed');
});

test('a modified click opens the full page', () => {
  page();
  const click = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0, ctrlKey: true});
  document.querySelector('[data-rel-new-open]').dispatchEvent(click);
  expect(click.defaultPrevented).toBe(false);
});
