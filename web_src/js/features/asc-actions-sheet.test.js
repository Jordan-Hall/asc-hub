import {initAscActionsSheet} from './asc-actions-sheet.js';

beforeEach(() => {
  document.body.innerHTML = `<button data-asc-actions-open>…</button>
<dialog data-asc-actions-sheet><h2>Change 3 · actions</h2>
<button class="rv-menu-item" data-asc-open-review="reject">Request changes</button>
<button class="rv-menu-item" data-clipboard-text="asc stack get 5">Copy asc command</button></dialog>`;
  const sheet = document.querySelector('dialog');
  sheet.showModal = vi.fn(() => sheet.setAttribute('open', ''));
  sheet.close = vi.fn(() => {
    sheet.removeAttribute('open');
    sheet.dispatchEvent(new Event('close'));
  });
});

test('the dots button opens the sheet on its first item', () => {
  initAscActionsSheet();
  document.querySelector('[data-asc-actions-open]').click();
  expect(document.querySelector('dialog').showModal).toHaveBeenCalled();
  expect(document.activeElement.textContent).toBe('Request changes');
});

test('an item closes the sheet and the focus goes back to the button', () => {
  initAscActionsSheet();
  document.querySelector('[data-asc-actions-open]').click();
  document.querySelectorAll('.rv-menu-item')[1].click();
  expect(document.querySelector('dialog').close).toHaveBeenCalled();
  expect(document.activeElement).toBe(document.querySelector('[data-asc-actions-open]'));
});

test('a page without the sheet is left alone', () => {
  document.body.innerHTML = '<button data-asc-actions-open></button>';
  expect(() => initAscActionsSheet()).not.toThrow();
});
