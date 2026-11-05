import {initImport, followImport} from './asc-welcome.js';
import {GET, POST} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn(), POST: vi.fn()}));

const page = `
<div class="site-started">
  <div data-gs-part="top"><span>2 of 4</span></div>
  <h2 data-gs-imp-h="Import a repository"><span>Bring a repository</span></h2>
  <div data-gs-choose><a href="/repo/migrate" data-gs-imp-open>Import</a></div>
  <form class="tw-hidden" action="/-/get-started/import" data-gs-imp data-status="/-/get-started/import/status"
    data-t-lost="The hub stopped answering." data-t-done="The import is done.">
    <input name="clone_addr" type="url">
    <p class="tw-hidden" data-gs-imp-err></p>
    <div class="tw-hidden" data-gs-imp-run><b data-gs-imp-count></b><progress max="1" data-gs-imp-bar></progress><p data-gs-imp-live></p></div>
    <button type="submit" data-gs-imp-go>Import</button>
    <button type="button" data-gs-imp-back>Back</button>
    <a class="tw-hidden" href="/" data-gs-imp-repo>Open the repository</a>
  </form>
</div>`;

const q = (sel) => document.querySelector(sel);
const json = (body, ok = true) => () => Promise.resolve({ok, json: () => Promise.resolve(body)});
const html = (body) => () => Promise.resolve({ok: true, text: () => Promise.resolve(body)});
const offline = () => Promise.reject(new Error('offline'));
const hidden = (sel) => q(sel).classList.contains('tw-hidden');
const submit = () => q('[data-gs-imp]').dispatchEvent(new Event('submit', {cancelable: true}));

beforeEach(() => {
  document.body.innerHTML = page;
  vi.mocked(GET).mockReset();
  vi.mocked(POST).mockReset();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

async function tick(ms) {
  await vi.advanceTimersByTimeAsync(ms);
}

test('the import opens in place of the choice and Back puts the choice back', () => {
  initImport(q('.site-started'), 100);
  q('[data-gs-imp-open]').click();
  expect(hidden('[data-gs-choose]')).toBe(true);
  expect(hidden('[data-gs-imp]')).toBe(false);
  expect(q('[data-gs-imp-h]').textContent).toBe('Import a repository');
  expect(document.activeElement).toBe(q('[name=clone_addr]'));
  q('[data-gs-imp-back]').click();
  expect(hidden('[data-gs-choose]')).toBe(false);
  expect(hidden('[data-gs-imp]')).toBe(true);
  expect(q('[data-gs-imp-h] span').textContent).toBe('Bring a repository');
  expect(document.activeElement).toBe(q('[data-gs-imp-open]'));
});

test('a refused address shows the reason and the form stays usable', async () => {
  vi.mocked(POST).mockImplementation(json({error: 'Use an https address.'}, false));
  initImport(q('.site-started'), 100);
  q('[name=clone_addr]').value = 'http://example.org/acme/nova.git';
  submit();
  await tick(0);
  const data = vi.mocked(POST).mock.calls[0][1].data;
  expect(data.get('clone_addr')).toBe('http://example.org/acme/nova.git');
  expect(data.get('inline')).toBe('1');
  expect(q('[data-gs-imp-err]').textContent).toBe('Use an https address.');
  expect(hidden('[data-gs-imp-err]')).toBe(false);
  expect(q('[name=clone_addr]').getAttribute('aria-invalid')).toBe('true');
  expect(q('[data-gs-imp-go]').disabled).toBe(false);
  expect(hidden('[data-gs-imp-run]')).toBe(true);
});

test('an import reads history, then shows the commits read and the repository', async () => {
  vi.mocked(POST).mockImplementation(json({repo: 'nova', link: '/user8/nova'}));
  const answers = [json({status: 'queued'}), json({status: 'running'}),
    json({status: 'done', commits: 12408, count: '12,408 commits', link: '/user8/nova'}), html(page)];
  vi.mocked(GET).mockImplementation(() => answers.shift()());
  initImport(q('.site-started'), 100);
  q('[name=clone_addr]').value = 'https://example.org/acme/nova.git';
  submit();
  await tick(0);
  expect(hidden('[data-gs-imp-run]')).toBe(false);
  expect(q('[data-gs-imp-bar]').hasAttribute('value')).toBe(false);
  expect(q('[data-gs-imp-go]').disabled).toBe(true);
  await tick(300);
  expect(GET).toHaveBeenNthCalledWith(1, '/-/get-started/import/status?repo=nova');
  expect(q('[data-gs-imp-count]').textContent).toBe('12,408 commits');
  expect(q('[data-gs-imp-bar]').value).toBe(1);
  expect(q('[data-gs-imp-live]').textContent).toBe('The import is done.');
  expect(q('[data-gs-imp-repo]').getAttribute('href')).toBe('/user8/nova');
  expect(hidden('[data-gs-imp-repo]')).toBe(false);
  expect(hidden('[data-gs-imp-go]')).toBe(true);
  expect(GET).toHaveBeenNthCalledWith(4, window.location.href);
});

test('a failed import shows the reason and lets the person try again', async () => {
  vi.mocked(GET).mockImplementation(json({status: 'failed', message: 'authentication failed'}));
  const form = q('[data-gs-imp]');
  q('[data-gs-imp-run]').classList.remove('tw-hidden');
  followImport(q('.site-started'), form, {name: 'nova', link: '/user8/nova'}, 100);
  await tick(100);
  expect(q('[data-gs-imp-err]').textContent).toBe('authentication failed');
  expect(hidden('[data-gs-imp-run]')).toBe(true);
  expect(q('[data-gs-imp-go]').disabled).toBe(false);
  await tick(1000);
  expect(GET).toHaveBeenCalledTimes(1);
});

test('a hub that stops answering is given up after three tries, with a link to the repository', async () => {
  vi.mocked(GET).mockImplementation(offline);
  followImport(q('.site-started'), q('[data-gs-imp]'), {name: 'nova', link: '/user8/nova'}, 100);
  await tick(200);
  expect(q('[data-gs-imp-err]').textContent).toBe('');
  await tick(100);
  expect(GET).toHaveBeenCalledTimes(3);
  expect(q('[data-gs-imp-err]').textContent).toBe('The hub stopped answering.');
  expect(q('[data-gs-imp-repo]').getAttribute('href')).toBe('/user8/nova');
  await tick(1000);
  expect(GET).toHaveBeenCalledTimes(3);
});
