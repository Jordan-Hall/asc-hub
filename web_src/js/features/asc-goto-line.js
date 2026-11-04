// "Go to line" of a file or its blame (board Code, key l): the button with data-goto-line-open opens the dialog with
// data-goto-line, and the number typed becomes the #L<n> anchor, which repo-code selects and scrolls to. A number past
// the end goes to the last line.

export function lineAnchor(value, root = document) {
  const n = Number(String(value).trim());
  if (!Number.isInteger(n) || n < 1) return '';
  if (root.querySelector(`.lines-num #L${n}`)) return `L${n}`;
  const lines = root.querySelectorAll('.lines-num [id^="L"]');
  return lines.length ? lines[lines.length - 1].id : '';
}

export function initAscGotoLine() {
  const dialog = document.querySelector('dialog[data-goto-line]');
  const open = document.querySelector('[data-goto-line-open]');
  const input = dialog?.querySelector('input');
  if (!open || !input || typeof dialog.showModal !== 'function') return;
  open.addEventListener('click', () => {
    input.value = '';
    dialog.showModal();
    input.focus();
  });
  dialog.addEventListener('close', () => {
    if (dialog.returnValue !== 'go') return;
    const id = lineAnchor(input.value);
    if (id) window.location.hash = id;
  });
}
