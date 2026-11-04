// The failure groups of the Checks page (templates/repo/asc_checks.tmpl): on a touch screen a card swiped to the left
// opens its Acknowledge, as the button on the card does (board Mobile-Checks). The button stays for everyone else.

export const SWIPE_ARM = 80; // px to the left that acknowledge on release
const SWIPE_MAX = 120;
const SWIPE_START = 10; // px before a move counts as a swipe and not a tap or a scroll

/** Where a horizontal drag of dx leaves the card: only to the left, no further than SWIPE_MAX. */
export function swipeOffset(dx) {
  return Math.max(-SWIPE_MAX, Math.min(0, dx));
}

function reset(card) {
  card.style.transform = '';
  card.classList.remove('is-swiping');
  delete card.dataset.swipe;
}

export function initAscChecksSwipe(root = document) {
  const list = root.querySelector('[data-testid="asc-groups"]');
  if (!list) return;
  let drag = null;

  list.addEventListener('pointerdown', (e) => {
    if (e.pointerType !== 'touch' || e.target.closest('a, button, summary, input, details[open]')) return;
    const card = e.target.closest('.rv-group');
    const ack = card?.querySelector('[data-testid="asc-group-ack"]');
    if (!ack) return;
    drag = {card, ack, id: e.pointerId, x: e.clientX, y: e.clientY, moving: false};
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
      drag.card.classList.add('is-swiping');
      drag.card.setPointerCapture?.(e.pointerId);
    }
    const off = swipeOffset(dx);
    drag.card.style.transform = `translateX(${off}px)`;
    if (off <= -SWIPE_ARM) drag.card.dataset.swipe = 'armed';
    else delete drag.card.dataset.swipe;
  });

  const end = (e) => {
    if (!drag || e.pointerId !== drag.id) return;
    const {card, ack, moving} = drag;
    drag = null;
    if (!moving) return;
    const armed = e.type === 'pointerup' && card.dataset.swipe === 'armed';
    reset(card);
    if (armed) ack.click();
  };
  list.addEventListener('pointerup', end);
  list.addEventListener('pointercancel', end);
}
