// The "On this page" row of a guide on a phone is a disclosure under the header: a tap on one of its links goes to the
// heading and closes it, so the text is not hidden behind the list.

export function initAscDocsToc() {
  const toc = document.querySelector('details.site-toc-m');
  if (!toc) return;
  toc.addEventListener('click', (e) => {
    if (e.target instanceof Element && e.target.closest('a[href^="#"]')) toc.open = false;
  });
}
