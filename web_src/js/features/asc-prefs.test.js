import {choiceOf, initAscPrefs, save} from './asc-prefs.js';
import {showErrorToast} from '../modules/toast.js';

vi.mock('../modules/toast.js', () => ({showErrorToast: vi.fn()}));

const page = () => `
<meta name="asc-prefs" content="/-/asc/prefs" data-failed="Could not save this choice to your account. Try again.">
<div role="group"><button type="button" data-ds-theme-set="dark"><span>Dark</span></button><button type="button" data-ds-theme-set="sepia">x</button></div>
<div role="group"><button type="button" data-ds-density-set="compact">Compact</button></div>
<div data-asc-density="#list"><button type="button" data-density="comfy">Comfy</button></div>
<button type="button" data-density="compact" id="loose">not in a switch</button>
<p id="other">text</p>`;

const q = (sel) => document.querySelector(sel);
const answer = (status) => Promise.resolve({ok: status >= 200 && status < 300, status});
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const sendOk = () => answer(204);
const sendFailing = () => answer(400);
const sendThrowing = () => Promise.reject(new Error('offline'));
const sent = (send) => Object.fromEntries(send.mock.calls[0][1].data);

beforeEach(() => {
  document.body.innerHTML = page();
  vi.mocked(showErrorToast).mockClear();
});

test('choiceOf reads the theme and density buttons', () => {
  expect(choiceOf(q('[data-ds-theme-set="dark"] span'))).toEqual({theme: 'dark'});
  expect(choiceOf(q('[data-ds-density-set]'))).toEqual({density: 'compact'});
  expect(choiceOf(q('[data-asc-density] button'))).toEqual({density: 'comfy'});
});

test('choiceOf ignores anything else', () => {
  expect(choiceOf(q('[data-ds-theme-set="sepia"]'))).toBeNull();
  expect(choiceOf(q('#loose'))).toBeNull();
  expect(choiceOf(q('#other'))).toBeNull();
  expect(choiceOf(null)).toBeNull();
});

test('save posts the choice to the account', async () => {
  const send = vi.fn(sendOk);
  expect(await save(q('meta[name="asc-prefs"]'), {theme: 'light'}, send)).toBe(true);
  expect(send.mock.calls[0][0]).toBe('/-/asc/prefs');
  expect(sent(send)).toEqual({theme: 'light'});
  expect(showErrorToast).not.toHaveBeenCalled();
});

test('a refused or failed save says so', async () => {
  expect(await save(q('meta[name="asc-prefs"]'), {density: 'compact'}, sendFailing)).toBe(false);
  expect(await save(q('meta[name="asc-prefs"]'), {density: 'compact'}, sendThrowing)).toBe(false);
  expect(showErrorToast).toHaveBeenCalledTimes(2);
  expect(showErrorToast).toHaveBeenCalledWith('Could not save this choice to your account. Try again.');
});

test('a click on a theme or density button saves it', async () => {
  const send = vi.fn(sendOk);
  initAscPrefs({doc: document, send});
  q('[data-ds-density-set]').click();
  q('#other').click();
  await settle();
  expect(send).toHaveBeenCalledTimes(1);
  expect(sent(send)).toEqual({density: 'compact'});
});

test('a guest saves nothing with an account', async () => {
  q('meta[name="asc-prefs"]').remove();
  const send = vi.fn(sendOk);
  initAscPrefs({doc: document, send});
  q('[data-ds-theme-set="dark"]').click();
  await settle();
  expect(send).not.toHaveBeenCalled();
});
