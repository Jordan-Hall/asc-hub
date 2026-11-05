import {fill, initAscBell, markShownRead} from './asc-bell.js';
import {showErrorToast} from '../modules/toast.js';

vi.mock('../modules/toast.js', () => ({showErrorToast: vi.fn()}));

const shell = () => `
<a class="ds-hdr-ic" href="/notifications" data-ds-open="ds-bell"><span class="notification_count">3</span></a>
<div class="ds-pop is-open" id="ds-bell" data-asc-bell="/-/asc/bell">
  <div data-asc-bell-body aria-busy="true">
    <div class="ds-bell-loading" data-asc-bell-loading></div>
    <p class="tw-hidden" role="alert" data-asc-bell-failed>Failed</p>
  </div>
</div>`;

const content = `<div data-asc-bell-content>
  <div class="ds-bell-head"><b>Notifications</b><span data-asc-bell-count>2</span>
    <button type="button" data-asc-bell-read="/-/asc/bell/read" data-failed="Could not mark them read.">Mark all read</button></div>
  <ul class="ds-bell-list">
    <li><a class="ds-bell-it is-unread" href="/-/asc/bell/open/1">Agent coder-2 finished<span class="ds-dot"></span><span class="tw-sr-only">Unread</span></a></li>
    <li><a class="ds-bell-it" href="/o/r/pulls/2">Priya approved</a></li>
  </ul>
  <div class="ds-bell-foot"><a href="/-/asc/inbox">Open inbox</a></div>
</div>`;

const q = (sel) => document.querySelector(sel);
const answer = (status, text = '') => Promise.resolve({ok: status >= 200 && status < 300, status, text: () => Promise.resolve(text)});
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const loadContent = () => answer(200, content);
const loadFailing = () => answer(500);
const loadThrowing = () => Promise.reject(new Error('offline'));
const sendOk = () => answer(204);
const sendFailing = () => answer(500);

beforeEach(() => {
  document.body.innerHTML = shell();
  vi.mocked(showErrorToast).mockClear();
});

test('fill puts the loaded inside into the popover', async () => {
  const load = vi.fn(loadContent);
  expect(await fill(q('#ds-bell'), load)).toBe(true);
  expect(load).toHaveBeenCalledWith('/-/asc/bell', {cache: 'no-store'});
  expect(document.querySelectorAll('.ds-bell-it')).toHaveLength(2);
  expect(q('[data-asc-bell-loading]')).toBeNull();
  expect(q('[data-asc-bell-body]').getAttribute('aria-busy')).toBe('false');
});

test('fill again replaces what was shown', async () => {
  await fill(q('#ds-bell'), loadContent);
  await fill(q('#ds-bell'), loadContent);
  expect(document.querySelectorAll('[data-asc-bell-content]')).toHaveLength(1);
});

test('a failed load shows the failed line', async () => {
  expect(await fill(q('#ds-bell'), loadFailing)).toBe(false);
  expect(q('[data-asc-bell-failed]').classList.contains('tw-hidden')).toBe(false);
  expect(q('[data-asc-bell-loading]').classList.contains('tw-hidden')).toBe(true);
});

test('a load that throws shows the failed line', async () => {
  expect(await fill(q('#ds-bell'), loadThrowing)).toBe(false);
  expect(q('[data-asc-bell-failed]').classList.contains('tw-hidden')).toBe(false);
});

test('markShownRead clears the unread marks and the header count', async () => {
  await fill(q('#ds-bell'), loadContent);
  markShownRead(document, q('#ds-bell'));
  expect(q('.is-unread')).toBeNull();
  expect(q('.ds-bell-it .ds-dot')).toBeNull();
  expect(q('.ds-bell-it .tw-sr-only')).toBeNull();
  expect(q('[data-asc-bell-count]')).toBeNull();
  expect(q('[data-asc-bell-read]')).toBeNull();
  expect(q('.notification_count').classList.contains('tw-hidden')).toBe(true);
  expect(q('.notification_count').textContent).toBe('0');
});

test('opening loads the inside and moves the focus into it', async () => {
  const load = vi.fn(loadContent);
  initAscBell({doc: document, load, send: sendOk});
  q('.ds-hdr-ic').focus();
  q('#ds-bell').dispatchEvent(new CustomEvent('ds-open'));
  await settle();
  await settle();
  expect(load).toHaveBeenCalledTimes(1);
  expect(document.activeElement).toBe(q('[data-asc-bell-read]'));
});

test('Mark all read posts and shows everything read', async () => {
  const send = vi.fn(sendOk);
  initAscBell({doc: document, load: loadContent, send});
  q('#ds-bell').dispatchEvent(new CustomEvent('ds-open'));
  await settle();
  await settle();
  q('[data-asc-bell-read]').click();
  await settle();
  await settle();
  expect(send).toHaveBeenCalledWith('/-/asc/bell/read');
  expect(q('.is-unread')).toBeNull();
  expect(document.activeElement).toBe(q('.ds-bell-it'));
  expect(showErrorToast).not.toHaveBeenCalled();
});

test('a failed Mark all read keeps the unread marks and says so', async () => {
  initAscBell({doc: document, load: loadContent, send: sendFailing});
  q('#ds-bell').dispatchEvent(new CustomEvent('ds-open'));
  await settle();
  await settle();
  q('[data-asc-bell-read]').click();
  await settle();
  await settle();
  expect(q('.is-unread')).not.toBeNull();
  expect(q('[data-asc-bell-read]').disabled).toBe(false);
  expect(showErrorToast).toHaveBeenCalledWith('Could not mark them read.');
});

test('no popover, nothing to do', () => {
  document.body.innerHTML = '';
  expect(() => initAscBell({doc: document, load: loadContent, send: sendOk})).not.toThrow();
});
