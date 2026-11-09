// The toast after "Apply as vN" (templates/repo/pulls/asc_applied_toast.tmpl) and after "Add to merge queue" on the
// dashboard is in the page without script. It leaves after 8 s (data-ms says otherwise), never while the pointer or
// the focus is on it, and on Esc; Undo is a real button in the tab order.

export const appliedToastMs = 8000;

export function initAscAppliedToast() {
  const toast = document.querySelector('[data-asc-applied-toast]');
  if (!toast) return;
  let timer = 0;
  const hold = () => window.clearTimeout(timer);
  const wait = () => {
    hold();
    timer = window.setTimeout(() => toast.remove(), Number(toast.getAttribute('data-ms')) || appliedToastMs);
  };
  toast.addEventListener('mouseenter', hold);
  toast.addEventListener('focusin', hold);
  toast.addEventListener('mouseleave', wait);
  toast.addEventListener('focusout', wait);
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') toast.remove();
  });
  wait();
}
