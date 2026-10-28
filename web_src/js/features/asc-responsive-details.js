// A <details data-asc-open-from="640"> is a menu on a narrow screen and plain content from that width up: it is
// forced open (and its summary is hidden by CSS) while the viewport is at least that many pixels wide, and keeps
// the choice of the person below it. Used for the Watch, Star and Fork buttons of a repository on a phone.

export function bindResponsiveDetails(el, mq) {
  const apply = () => {
    if (mq.matches) el.open = true;
    else el.open = false;
  };
  apply();
  mq.addEventListener('change', apply);
  return apply;
}

export function initAscResponsiveDetails() {
  for (const el of document.querySelectorAll('details[data-asc-open-from]')) {
    const px = Number.parseInt(el.getAttribute('data-asc-open-from'), 10);
    if (!Number.isFinite(px) || px <= 0) continue;
    bindResponsiveDetails(el, window.matchMedia(`(min-width: ${px}px)`));
  }
  // a menu on a phone closes when the page is touched elsewhere
  document.addEventListener('click', (e) => {
    for (const el of document.querySelectorAll('details[data-asc-open-from][open]')) {
      const px = Number.parseInt(el.getAttribute('data-asc-open-from'), 10);
      if (window.matchMedia(`(min-width: ${px}px)`).matches) continue;
      if (e.target instanceof Node && !el.contains(e.target)) el.open = false;
    }
  });
}
