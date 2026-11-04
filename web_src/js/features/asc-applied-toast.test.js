import {appliedToastMs, initAscAppliedToast} from './asc-applied-toast.js';

const page = () => {
  document.body.innerHTML = `<div class="rv-undo-toast" role="status" data-asc-applied-toast><div><b>Applied as v5</b></div>
    <form><button type="submit">Undo</button></form></div>`;
  return document.querySelector('[data-asc-applied-toast]');
};

beforeEach(() => vi.useFakeTimers());
afterEach(() => vi.useRealTimers());

test('the toast leaves after 8 s', () => {
  const toast = page();
  initAscAppliedToast();
  vi.advanceTimersByTime(appliedToastMs - 1);
  expect(toast.isConnected).toBe(true);
  vi.advanceTimersByTime(1);
  expect(toast.isConnected).toBe(false);
});

test('the toast stays while the pointer or the focus is on it', () => {
  const toast = page();
  initAscAppliedToast();
  toast.dispatchEvent(new Event('mouseenter'));
  vi.advanceTimersByTime(appliedToastMs * 2);
  expect(toast.isConnected).toBe(true);
  toast.dispatchEvent(new Event('mouseleave'));
  vi.advanceTimersByTime(appliedToastMs);
  expect(toast.isConnected).toBe(false);
});

test('Esc closes the toast', () => {
  const toast = page();
  initAscAppliedToast();
  document.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape'}));
  expect(toast.isConnected).toBe(false);
});

test('no toast, nothing to do', () => {
  document.body.innerHTML = '';
  expect(() => initAscAppliedToast()).not.toThrow();
});
