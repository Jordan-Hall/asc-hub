import $ from 'jquery';

/** Opens the hub's confirm modal for a form and calls `approve` when the person says yes. The modal gets the typed name. */
export function openConfirmModal(form, approve) {
  const $dialog = $(form.getAttribute('data-confirm-modal'));
  $dialog.find('.name').text(form.querySelector('[data-confirm-name]')?.value ?? '');
  $dialog.modal({closable: false, onApprove: approve}).modal('show');
}

/**
 * Asks before a form is sent: a form with data-confirm-modal="#id" opens that modal first and is sent only when the person
 * confirms (also when it was sent with the Enter key). Returns true when the send was stopped.
 */
export function onSubmitConfirm(e, open = openConfirmModal) {
  const form = e.target?.closest?.('form[data-confirm-modal]');
  if (!form) return false;
  if (form.hasAttribute('data-confirmed')) { // the send that the modal let through
    form.removeAttribute('data-confirmed');
    return false;
  }
  e.preventDefault();
  open(form, () => {
    form.setAttribute('data-confirmed', '');
    form.requestSubmit();
  });
  return true;
}

/** Removes an offer (an Undo) once its time is over: anything with data-undo-ms is taken away after that many ms. */
export function expireUndos(root = document, schedule = (fn, ms) => setTimeout(fn, ms)) {
  let n = 0;
  for (const el of root.querySelectorAll('[data-undo-ms]')) {
    const ms = Number(el.getAttribute('data-undo-ms'));
    if (!Number.isFinite(ms) || ms < 0 || ms > 600000) continue;
    schedule(() => el.remove(), ms);
    n++;
  }
  return n;
}

export function initAscConfirm() {
  document.addEventListener('submit', (e) => { onSubmitConfirm(e) });
  expireUndos();
}
