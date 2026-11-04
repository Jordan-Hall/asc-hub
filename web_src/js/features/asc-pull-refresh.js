// Pull to refresh on pages that mark their content with data-asc-pull-refresh (the agent run page, board
// Mobile-AgentRun). A browser tab has its own pull to refresh; the installed app (display-mode standalone) has none, so
// there a pull down from the top of the page past PULL_ARM reloads it. data-asc-ptr is the arrow that shows the pull.

export const PULL_ARM = 70; // px of pull that reload on release
const PULL_MAX = 110;

/** How far the arrow comes down for a pull of dy: half the finger, no further than PULL_MAX. */
export function pullOffset(dy) {
  return Math.max(0, Math.min(PULL_MAX, dy / 2));
}

export function initAscPullRefresh(doc = document, win = window) {
  const area = doc.querySelector('[data-asc-pull-refresh]');
  const arrow = doc.querySelector('[data-asc-ptr]');
  if (!area || !arrow || !win.matchMedia?.('(display-mode: standalone)').matches) return;
  let start = null;
  let pulled = 0;

  const show = (dy) => {
    pulled = dy;
    const off = pullOffset(dy);
    arrow.style.transform = off ? `translateY(${off}px) rotate(${off * 3}deg)` : '';
    arrow.classList.toggle('is-pulling', off > 0);
    arrow.classList.toggle('is-armed', off * 2 >= PULL_ARM);
  };

  area.addEventListener('touchstart', (e) => {
    start = win.scrollY <= 0 && e.touches.length === 1 ? e.touches[0].clientY : null;
    pulled = 0;
  }, {passive: true});
  area.addEventListener('touchmove', (e) => {
    if (start === null) return;
    const dy = e.touches[0].clientY - start;
    if (dy <= 0 || win.scrollY > 0) {
      start = null;
      show(0);
      return;
    }
    show(dy);
  }, {passive: true});
  const end = (e) => {
    if (start === null) return;
    start = null;
    const reload = e.type === 'touchend' && pullOffset(pulled) * 2 >= PULL_ARM;
    if (reload) {
      arrow.classList.add('is-loading');
      win.location.reload();
      return;
    }
    show(0);
  };
  area.addEventListener('touchend', end);
  area.addEventListener('touchcancel', end);
}
