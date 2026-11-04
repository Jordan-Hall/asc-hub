import {showModal} from '../modules/modal.ts';

/** Shows the dialogs that a refused form asked for again: every dialog[data-asc-open] opens on load. */
export function reopenDialogs(root = document, open = (d) => showModal(d, undefined)) {
  let n = 0;
  for (const d of root.querySelectorAll('dialog[data-asc-open]')) {
    if (d.open) continue;
    open(d);
    n++;
  }
  return n;
}

/** Closes the dialog of a header close button ([data-asc-close]); the dialog's Cancel button is wired by showModal. */
export function onCloseClick(e) {
  const btn = e.target?.closest?.('[data-asc-close]');
  if (!btn) return false;
  const d = btn.closest('dialog');
  if (!d) return false;
  e.preventDefault();
  d.close();
  return true;
}

export function initAscDialog() {
  document.addEventListener('click', (e) => { onCloseClick(e) });
  reopenDialogs();
}
