/** Asks before a form is sent: a form with data-submit-confirm shows its text in the browser's confirm dialog. */
export function onSubmitConfirm(e, ask = (text) => window.confirm(text)) {
  const form = e.target?.closest?.('form[data-submit-confirm]');
  if (!form) return false;
  if (ask(form.getAttribute('data-submit-confirm') || '')) return false;
  e.preventDefault();
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
