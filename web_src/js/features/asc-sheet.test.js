import {CLOSE_PX, initAscSheets} from './asc-sheet.js';

function mount() {
  document.body.innerHTML = `<dialog class="rp-sheet" open><div class="rp-sheet-grip"></div><h2 class="rp-sheet-h">T</h2><p>body</p></dialog>`;
  const dialog = document.querySelector('dialog');
  dialog.close = vi.fn();
  initAscSheets();
  return dialog;
}

function drag(from, dy) {
  from.dispatchEvent(new MouseEvent('pointerdown', {bubbles: true, clientY: 100}));
  from.dispatchEvent(new MouseEvent('pointermove', {bubbles: true, clientY: 100 + dy}));
  from.dispatchEvent(new MouseEvent('pointerup', {bubbles: true, clientY: 100 + dy}));
}

test('dragging the grip down far enough closes the sheet', () => {
  const dialog = mount();
  drag(dialog.querySelector('.rp-sheet-grip'), CLOSE_PX + 20);
  expect(dialog.close).toHaveBeenCalledTimes(1);
  expect(dialog.style.transform).toBe('');
});

test('a short drag, or one on the body, keeps it open', () => {
  const dialog = mount();
  drag(dialog.querySelector('.rp-sheet-h'), CLOSE_PX - 20);
  drag(dialog.querySelector('p'), CLOSE_PX + 20);
  expect(dialog.close).not.toHaveBeenCalled();
});

test('a tap on the backdrop closes it, a tap inside does not', () => {
  const dialog = mount();
  dialog.querySelector('p').click();
  expect(dialog.close).not.toHaveBeenCalled();
  dialog.click();
  expect(dialog.close).toHaveBeenCalledTimes(1);
});
