// A region that scrolls sideways (a code block of a guide, a wide table) must take the keyboard: without it a person
// who does not use a mouse cannot read the cut-off part. Each such element gets tabindex 0 while it scrolls.

const SELECTOR = '.markup pre, .site-table, .ds-term-bd, [data-asc-scroll]';

export function needsFocus(el) {
  return el.scrollWidth > el.clientWidth + 1 || el.scrollHeight > el.clientHeight + 1;
}

export function applyScrollFocus(root = document) {
  for (const el of root.querySelectorAll(SELECTOR)) {
    const scrolls = needsFocus(el);
    if (scrolls && !el.hasAttribute('tabindex')) {
      el.setAttribute('tabindex', '0');
      el.setAttribute('data-asc-scroll-focus', '');
    } else if (!scrolls && el.hasAttribute('data-asc-scroll-focus')) {
      el.removeAttribute('tabindex');
      el.removeAttribute('data-asc-scroll-focus');
    }
  }
}

export function initAscScrollFocus() {
  applyScrollFocus();
  let timer;
  window.addEventListener('resize', () => {
    clearTimeout(timer);
    timer = setTimeout(() => applyScrollFocus(), 150);
  });
}
