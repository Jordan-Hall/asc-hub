import {initAscDocsFeedback} from './asc-docs-feedback.js';

const html = `<section data-asc-docs-feedback="install">
  <form method="post" action="/-/docs/install/feedback">
    <input type="hidden" name="_csrf" value="t">
    <button type="submit" name="helpful" value="yes">Yes</button>
    <button type="submit" name="helpful" value="no">No</button>
  </form>
  <p role="status" data-thanks="Thanks." data-failed="Not sent.">Feedback goes to the docs team</p>
</section>`;

const q = (sel) => document.querySelector(sel);
const settle = () => new Promise((resolve) => setTimeout(resolve, 0));
const memory = () => {
  const m = new Map();
  return {getItem: (k) => m.get(k) ?? null, setItem: (k, v) => m.set(k, v), m};
};
const press = (value) => q('form').requestSubmit(q(`button[value="${value}"]`));

beforeEach(() => {
  document.body.innerHTML = html;
});

test('Yes is sent without leaving the page, pressed, and remembered', async () => {
  const storage = memory();
  const send = vi.fn(() => Promise.resolve({ok: true, status: 200}));
  initAscDocsFeedback({storage, send});
  press('yes');
  await settle();
  expect(send).toHaveBeenCalledTimes(1);
  const [url, {data}] = send.mock.calls[0];
  expect(url).toBe('/-/docs/install/feedback');
  expect(data.get('helpful')).toBe('yes');
  expect(data.get('_csrf')).toBe('t');
  expect(q('button[value="yes"]').getAttribute('aria-pressed')).toBe('true');
  expect(q('button[value="no"]').getAttribute('aria-pressed')).toBe('false');
  expect(Array.from(document.querySelectorAll('button'), (b) => b.disabled)).toEqual([true, true]);
  expect(q('[role="status"]').textContent).toBe('Thanks.');
  expect(storage.m.get('asc-docs-feedback:install')).toBe('yes');
});

test('a remembered answer shows at once and is not asked again', () => {
  const storage = memory();
  storage.setItem('asc-docs-feedback:install', 'no');
  initAscDocsFeedback({storage, send: vi.fn()});
  expect(q('button[value="no"]').getAttribute('aria-pressed')).toBe('true');
  expect(q('button[value="yes"]').disabled).toBe(true);
  expect(q('[role="status"]').textContent).toBe('Thanks.');
});

test('a failed send keeps the buttons and says so', async () => {
  const storage = memory();
  const send = vi.fn(() => Promise.resolve({ok: false, status: 429}));
  initAscDocsFeedback({storage, send});
  press('no');
  await settle();
  expect(q('button[value="no"]').disabled).toBe(false);
  expect(q('button[value="no"]').hasAttribute('aria-pressed')).toBe(false);
  expect(q('[role="status"]').textContent).toBe('Not sent.');
  expect(storage.m.size).toBe(0);
});

test('blocked storage still sends', async () => {
  const storage = {getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') }};
  const send = vi.fn(() => Promise.resolve({ok: true, status: 200}));
  initAscDocsFeedback({storage, send});
  press('yes');
  await settle();
  expect(q('[role="status"]').textContent).toBe('Thanks.');
});

test('a page without the section does nothing', () => {
  document.body.innerHTML = '<p>docs</p>';
  expect(() => initAscDocsFeedback({storage: memory(), send: vi.fn()})).not.toThrow();
});
