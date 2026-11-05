import {swapParts, watch, initAscWelcome} from './asc-welcome.js';
import {GET} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

const page = (connected) => `
<div class="site-started"${connected ? '' : ' data-gs-watch="/-/get-started/status"'}>
  <p role="status" data-gs-live data-t-connected="The command line signed in. Step 2 is done."></p>
  <div data-gs-part="top"><span>${connected ? '2' : '0'} of 4</span><progress max="4" value="${connected ? 2 : 0}"></progress></div>
  <ol data-gs-part="steps">
    <li class="${connected ? 'is-done' : ''}">1</li>
    <li class="${connected ? 'is-done' : 'is-current'}">2</li>
    <li class="${connected ? 'is-current' : ''}">3</li>
  </ol>
  <aside><p data-keep>kept</p></aside>
</div>`;

const q = (sel) => document.querySelector(sel);
const json = (body) => Promise.resolve({ok: true, json: () => Promise.resolve(body)});
const html = (body) => Promise.resolve({ok: true, text: () => Promise.resolve(body)});
const failing = () => Promise.resolve({ok: false, status: 500});
const offline = () => Promise.reject(new Error('offline'));

beforeEach(() => {
  document.body.innerHTML = page(false);
  vi.mocked(GET).mockReset();
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

async function tick(ms) {
  await vi.advanceTimersByTimeAsync(ms);
}

test('swapParts replaces the parts by name and keeps the rest', () => {
  const next = new DOMParser().parseFromString(page(true), 'text/html');
  swapParts(q('.site-started'), next);
  expect(q('[data-gs-part="steps"] li:nth-child(2)').classList.contains('is-done')).toBe(true);
  expect(q('[data-gs-part="steps"] li:nth-child(3)').classList.contains('is-current')).toBe(true);
  expect(q('progress').value).toBe(2);
  expect(q('[data-keep]')).not.toBeNull();
});

test('watch waits, then ticks step 2 once the command line signs in', async () => {
  const answers = [json({connected: false, done: 0, total: 4}), json({connected: true, done: 2, total: 4}), html(page(true))];
  vi.mocked(GET).mockImplementation(() => answers.shift());
  const root = q('.site-started');
  watch(root, '/-/get-started/status', 1000);
  await tick(1000);
  expect(GET).toHaveBeenCalledTimes(1);
  expect(q('[data-gs-part="steps"] li:nth-child(2)').classList.contains('is-current')).toBe(true);
  await tick(1000);
  expect(GET).toHaveBeenNthCalledWith(2, '/-/get-started/status');
  expect(GET).toHaveBeenNthCalledWith(3, window.location.href);
  expect(q('[data-gs-part="steps"] li:nth-child(2)').classList.contains('is-done')).toBe(true);
  expect(q('[data-gs-live]').textContent).toBe('The command line signed in. Step 2 is done.');
  expect(root.hasAttribute('data-gs-watch')).toBe(false);
  await tick(5000);
  expect(GET).toHaveBeenCalledTimes(3);
});

test('a failed answer is asked again', async () => {
  const answers = [failing, offline, () => json({connected: true}), () => html(page(true))];
  vi.mocked(GET).mockImplementation(() => answers.shift()());
  watch(q('.site-started'), '/s', 1000);
  await tick(3000);
  expect(GET).toHaveBeenCalledTimes(4);
  expect(q('[data-gs-live]').textContent).not.toBe('');
});

test('a hidden tab does not ask', async () => {
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  watch(q('.site-started'), '/s', 1000);
  await tick(3000);
  expect(GET).not.toHaveBeenCalled();
  hidden.mockRestore();
});

test('initAscWelcome watches only a waiting checklist', async () => {
  document.body.innerHTML = page(true);
  initAscWelcome();
  await tick(10000);
  expect(GET).not.toHaveBeenCalled();
});
