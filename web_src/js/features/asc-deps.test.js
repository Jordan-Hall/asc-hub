import {GET} from '../modules/fetch.js';
import {initAscDeps, outdatedText} from './asc-deps.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

function page() {
  document.body.innerHTML = `<a data-deps-outdated data-url="/o/r/asc/deps/outdated" href="/o/r/asc/deps?view=updates">
    <div data-deps-outdated-n>–</div>
    <div data-deps-outdated-sub data-loading="checking" data-security="%d with a security fix" data-unknown="%d could not be checked" data-failed="could not check">Newer versions</div>
  </a>
  <button type="button" hidden aria-expanded="false" aria-controls="asc-dep-detail-7" data-dep-toggle>Details</button>
  <div id="asc-dep-detail-7" data-dep-detail>Why it matters</div>`;
}

const flush = () => new Promise((resolve) => setTimeout(resolve, 0));

test('the outdated line names the security fixes and the unchecked packages', () => {
  const sub = document.createElement('div');
  sub.setAttribute('data-security', '%d with a security fix');
  sub.setAttribute('data-unknown', '%d could not be checked');
  expect(outdatedText(sub, {outdated: 11, security: 3, unknown: 0})).toBe('3 with a security fix');
  expect(outdatedText(sub, {outdated: 11, security: 3, unknown: 2})).toBe('3 with a security fix · 2 could not be checked');
  expect(outdatedText(sub, {outdated: 0, security: 0, unknown: 0})).toBe('');
});

test('a finding starts closed with scripts and the button opens and closes it', () => {
  GET.mockResolvedValue({ok: true, json: () => Promise.resolve({outdated: 0, security: 0, unknown: 0})});
  page();
  initAscDeps();
  const button = document.querySelector('[data-dep-toggle]');
  const detail = document.querySelector('[data-dep-detail]');
  expect(button.hidden).toBe(false);
  expect(detail.hidden).toBe(true);
  button.click();
  expect(button.getAttribute('aria-expanded')).toBe('true');
  expect(detail.hidden).toBe(false);
  button.click();
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(detail.hidden).toBe(true);
});

test('the outdated tile fills from the registries', async () => {
  GET.mockResolvedValue({ok: true, json: () => Promise.resolve({outdated: 11, security: 3, unknown: 0})});
  page();
  initAscDeps();
  expect(GET).toHaveBeenCalledWith('/o/r/asc/deps/outdated');
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('checking');
  await flush();
  expect(document.querySelector('[data-deps-outdated-n]').textContent).toBe('11');
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('3 with a security fix');
});

test('a failed check is said as such and never drawn as zero', async () => {
  GET.mockResolvedValue({ok: false, status: 502});
  page();
  initAscDeps();
  await flush();
  expect(document.querySelector('[data-deps-outdated-n]').textContent).toBe('–');
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('could not check');
  GET.mockRejectedValue(new Error('offline'));
  page();
  initAscDeps();
  await flush();
  expect(document.querySelector('[data-deps-outdated-sub]').textContent).toBe('could not check');
});
