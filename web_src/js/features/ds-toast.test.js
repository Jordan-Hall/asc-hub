import {showDsToast} from './ds-toast.js';

test('a toast shows its text in a live region and leaves on dismiss', () => {
  const t = showDsToast('Copied');
  const region = document.querySelector('.ds-toasts');
  expect(region.getAttribute('aria-live')).toBe('polite');
  expect(region.textContent).toBe('Copied');
  t.dismiss();
  expect(region.textContent).toBe('');
});

test('the action runs once and closes the toast', () => {
  const onAction = vi.fn();
  showDsToast('Switched', {actionLabel: 'Undo', onAction, duration: 0});
  document.querySelector('.ds-toast-action').click();
  expect(onAction).toHaveBeenCalledTimes(1);
  expect(document.querySelector('.ds-toast')).toBeNull();
});

test('a toast closes by itself after its duration', () => {
  vi.useFakeTimers();
  showDsToast('Saved', {duration: 1000});
  expect(document.querySelector('.ds-toast')).not.toBeNull();
  vi.advanceTimersByTime(1001);
  expect(document.querySelector('.ds-toast')).toBeNull();
  vi.useRealTimers();
});
