// "On this page" of a documentation article: the entry of the section in view gets aria-current="location". The list is
// .site-toc [data-toc] with links to the ids of headings; without IntersectionObserver nothing is marked.

export function pickActive(tops, offset = 96) {
  // tops: [{id, top}] in document order; the last heading whose top has passed the offset is the section in view
  let active = null;
  for (const t of tops) {
    if (t.top - offset <= 0) active = t.id;
    else break;
  }
  return active ?? tops[0]?.id ?? null;
}

const sideKey = 'asc-docs-side';

function safeGet(storage) {
  try {
    return storage.getItem(sideKey);
  } catch {
    return null;
  }
}

/**
 * The guides beside an article keep their scroll position from page to page: it is kept for the tab when a page is left
 * and put back when the next one opens; the page open is then brought into view if it is not.
 */
export function keepSideScroll(side, storage = window.sessionStorage) {
  const top = Number(safeGet(storage));
  if (top > 0) side.scrollTop = top;
  side.querySelector('a[aria-current="page"]')?.scrollIntoView?.({block: 'nearest'});
  window.addEventListener('pagehide', () => {
    try {
      storage.setItem(sideKey, String(Math.round(side.scrollTop)));
    } catch {
      // storage is blocked: the list opens at its top
    }
  });
}

export function initAscDocs() {
  const side = document.querySelector('[data-asc-docs-side]');
  if (side) keepSideScroll(side);
  const toc = document.querySelector('.site-toc [data-toc]');
  if (!toc) return;
  const links = Array.from(toc.querySelectorAll('a[href^="#"]'));
  const heads = links.map((a) => document.getElementById(decodeURIComponent(a.getAttribute('href').slice(1)))).filter(Boolean);
  if (!heads.length) return;
  const mark = () => {
    const id = pickActive(heads.map((h) => ({id: h.id, top: h.getBoundingClientRect().top})));
    for (const a of links) {
      if (decodeURIComponent(a.getAttribute('href').slice(1)) === id) a.setAttribute('aria-current', 'location');
      else a.removeAttribute('aria-current');
    }
  };
  let frame = 0;
  window.addEventListener('scroll', () => {
    cancelAnimationFrame(frame);
    frame = requestAnimationFrame(mark);
  }, {passive: true});
  mark();
}
