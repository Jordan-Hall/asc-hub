// The bottom sheets of the repository pages (dialog.rp-sheet): dragging the grip or the heading down by more than
// CLOSE_PX closes the sheet, a shorter drag springs back, and a tap on the backdrop closes it.

export const CLOSE_PX = 80;

export function dragOutcome(dy) {
  return dy > CLOSE_PX ? 'close' : 'stay';
}

function wire(dialog) {
  let startY = null;
  const reset = () => {
    startY = null;
    dialog.style.removeProperty('transform');
  };
  dialog.addEventListener('pointerdown', (e) => {
    if (!e.target.closest?.('.rp-sheet-grip, .rp-sheet-h')) return;
    startY = e.clientY;
    e.target.setPointerCapture?.(e.pointerId);
  });
  dialog.addEventListener('pointermove', (e) => {
    if (startY === null) return;
    dialog.style.transform = `translateY(${Math.max(0, e.clientY - startY)}px)`;
  });
  dialog.addEventListener('pointerup', (e) => {
    if (startY === null) return;
    const close = dragOutcome(e.clientY - startY) === 'close';
    reset();
    if (close) dialog.close();
  });
  dialog.addEventListener('pointercancel', reset);
  // a click on the dialog itself, not its content, is a click on the backdrop
  dialog.addEventListener('click', (e) => {
    if (e.target === dialog) dialog.close();
  });
}

export function initAscSheets() {
  for (const dialog of document.querySelectorAll('dialog.rp-sheet')) wire(dialog);
}
