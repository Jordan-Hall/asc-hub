// Rows of the dashboard's "Needs you" and of the inbox (board Mobile-Dashboard, Mobile-Inbox): on a touch screen a row
// swiped to the right is marked done, one swiped to the left is snoozed for four hours. Each row carries a hidden form
// for its pull request (form[data-asc-row-state]); e on a row that has the focus marks it done with the same form. The
// buttons of the inbox and its detail pane stay for everyone else.

export const SWIPE_ARM = 80; // px either way that act on release
const SWIPE_MAX = 120;
const SWIPE_START = 10; // px before a move counts as a swipe and not a tap or a scroll

/** Where a horizontal drag of dx leaves the row: no further than SWIPE_MAX either way. */
export function swipeOffset(dx) {
  return Math.max(-SWIPE_MAX, Math.min(SWIPE_MAX, dx));
}

/** What a swipe of off does on release: done to the right, snooze to the left, nothing short of SWIPE_ARM. */
export function swipeAction(off) {
  if (off >= SWIPE_ARM) return 'done';
  if (off <= -SWIPE_ARM) return 'snooze';
  return '';
}

/** Sends the state form of a row with do set to action (done or snooze). */
export function sendRowState(form, action) {
  const field = form.querySelector('input[name="do"]');
  if (!field) return;
  field.value = action;
  form.requestSubmit();
}

/** The state form of the row that holds el, if any. */
export function rowStateForm(el) {
  return el?.closest?.('[data-asc-swipe-row]')?.querySelector('form[data-asc-row-state]') ?? null;
}

function reset(row) {
  row.style.transform = '';
  row.classList.remove('is-swiping');
  delete row.dataset.swipe;
}

export function initAscRowSwipe(root = document) {
  for (const list of root.querySelectorAll('[data-asc-swipe]')) {
    let drag = null;

    list.addEventListener('pointerdown', (e) => {
      if (e.pointerType !== 'touch' || e.target.closest('button, summary, input, label, details[open]')) return;
      const row = e.target.closest('[data-asc-swipe-row]');
      const form = rowStateForm(row);
      if (!form) return;
      drag = {row, form, id: e.pointerId, x: e.clientX, y: e.clientY, moving: false};
    });

    list.addEventListener('pointermove', (e) => {
      if (!drag || e.pointerId !== drag.id) return;
      const dx = e.clientX - drag.x;
      const dy = e.clientY - drag.y;
      if (!drag.moving) {
        if (Math.abs(dy) > SWIPE_START && Math.abs(dy) > Math.abs(dx)) {
          drag = null; // a scroll
          return;
        }
        if (Math.abs(dx) < SWIPE_START) return;
        drag.moving = true;
        drag.row.classList.add('is-swiping');
        drag.row.setPointerCapture?.(e.pointerId);
      }
      const off = swipeOffset(dx);
      drag.row.style.transform = `translateX(${off}px)`;
      const action = swipeAction(off);
      if (action) drag.row.dataset.swipe = action;
      else delete drag.row.dataset.swipe;
    });

    // a swipe is not a tap: the link under the finger is not followed
    list.addEventListener('click', (e) => {
      if (!list.dataset.swiped) return;
      delete list.dataset.swiped;
      e.preventDefault();
    }, true);

    const end = (e) => {
      if (!drag || e.pointerId !== drag.id) return;
      const {row, form, moving} = drag;
      drag = null;
      if (!moving) return;
      list.dataset.swiped = '1';
      setTimeout(() => delete list.dataset.swiped, 0);
      const action = e.type === 'pointerup' ? row.dataset.swipe : '';
      reset(row);
      if (action) sendRowState(form, action);
    };
    list.addEventListener('pointerup', end);
    list.addEventListener('pointercancel', end);
  }
}
