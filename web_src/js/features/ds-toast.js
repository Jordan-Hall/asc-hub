// The toast of the ASC design system (board A1, "banners, flash, toast"): a dark inverse-surface card in a live
// region at the bottom of the page. It reports a change that already happened (a copy, a switch that applied
// at once) and may carry one action, usually Undo. It leaves by itself after a few seconds, on Esc and on its
// close button; the action works by keyboard because the button is a real button in the tab order.
//
// Markup does not need script: <button data-ds-toast="Copied" data-clipboard-text="..."> raises a toast on click.

const defaultMs = 5000;
let region = null;

function toastRegion() {
  if (region && document.body.contains(region)) return region;
  region = document.createElement('div');
  region.className = 'ds-toasts';
  region.setAttribute('role', 'status');
  region.setAttribute('aria-live', 'polite');
  region.setAttribute('aria-atomic', 'false');
  document.body.append(region);
  return region;
}

// showDsToast(message, {actionLabel, onAction, closeLabel, duration}) returns {dismiss}. A duration of 0 keeps
// the toast until it is closed.
export function showDsToast(message, {actionLabel = '', onAction = null, closeLabel = '', duration = defaultMs} = {}) {
  const el = document.createElement('div');
  el.className = 'ds-toast';
  const text = document.createElement('span');
  text.className = 'ds-toast-text';
  text.textContent = message;
  el.append(text);
  let timer = 0;
  const dismiss = () => {
    window.clearTimeout(timer);
    el.remove();
  };
  if (actionLabel && onAction) {
    const act = document.createElement('button');
    act.type = 'button';
    act.className = 'ds-toast-action';
    act.textContent = actionLabel;
    act.addEventListener('click', () => {
      dismiss();
      onAction();
    });
    el.append(act);
  }
  if (closeLabel) {
    const close = document.createElement('button');
    close.type = 'button';
    close.className = 'ds-toast-close';
    close.setAttribute('aria-label', closeLabel);
    close.textContent = '×';
    close.addEventListener('click', dismiss);
    el.append(close);
  }
  toastRegion().append(el);
  if (duration > 0) timer = window.setTimeout(dismiss, duration);
  return {dismiss};
}

export function initDsToast() {
  document.addEventListener('click', (e) => {
    const t = e.target instanceof Element ? e.target.closest('[data-ds-toast]') : null;
    if (!t) return;
    const message = t.getAttribute('data-ds-toast');
    if (message) showDsToast(message);
  });
  document.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape' || !region) return;
    const last = region.lastElementChild;
    if (last) last.remove();
  });
}
