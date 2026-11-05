// The code intelligence panel of the search page (board Search). On a wide screen a symbol result or a
// "Symbols matched" chip carrying data-search-panel loads the definition, references and callers into the
// right column, aside#asc-search-panel, instead of leaving the page. A narrow screen, a modified click or a
// failed load follows the link, so the page works the same without scripts.
import {GET} from '../modules/fetch.js';

export const panelWide = '(min-width: 1200px)';

// panelNode returns the .asc-panel element of a fragment the server answered, or null.
export function panelNode(html) {
  const doc = new DOMParser().parseFromString(html, 'text/html');
  return doc.querySelector('.asc-panel');
}

function plainClick(e) {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey;
}

export function initAscSearchPanel(root = document) {
  const aside = root.querySelector('#asc-search-panel');
  if (!aside) return;
  let seq = 0;
  let current = null;
  const status = (text) => {
    const p = document.createElement('p');
    p.className = 'asc-muted';
    p.textContent = text;
    aside.replaceChildren(p);
  };
  root.addEventListener('click', async (e) => {
    const link = e.target.closest?.('a[data-search-panel]');
    if (!link || !plainClick(e) || !window.matchMedia(panelWide).matches) return;
    e.preventDefault();
    const n = ++seq;
    current?.removeAttribute('aria-current');
    current = link;
    link.setAttribute('aria-current', 'true');
    aside.setAttribute('aria-busy', 'true');
    status(aside.getAttribute('data-loading'));
    try {
      const res = await GET(link.getAttribute('data-search-panel'));
      const node = panelNode(await res.text());
      if (n !== seq) return;
      if (node) aside.replaceChildren(node);
      else status(aside.getAttribute('data-failed'));
    } catch {
      if (n === seq) status(aside.getAttribute('data-failed'));
    } finally {
      if (n === seq) aside.removeAttribute('aria-busy');
    }
  });
}
