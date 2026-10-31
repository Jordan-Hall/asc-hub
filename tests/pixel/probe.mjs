// Functions that run inside the browser page. They are serialised by Playwright, so they must not refer to
// anything outside their own body.

/** Hides the "States" blocks under a board's main page, so the board shows the page and its footer only. */
export function hideBoardStates() {
  for (const label of document.querySelectorAll('.sect-label')) {
    const wrapper = label.parentElement;
    // never hide the block that holds the page title: that would be the page itself
    if (wrapper && !wrapper.querySelector('h1, header[role=banner]')) wrapper.style.display = 'none';
  }
  // a phone board without state labels lists its states one after the other, each in a .mscr section with a caption.
  // The page is the first; a later state that is a variant of it (a sheet, loading, empty, error) is not on the live page
  if (!document.querySelector('.sect-label')) {
    const variant = /sheet|loading|empty|error|no results|no [a-z]+ yet|offline|expired|denied|dialog|modal|skeleton|failed/i;
    for (const s of [...document.querySelectorAll('.mscr')].slice(1)) {
      const cap = s.querySelector('.mcap');
      if (cap && variant.test(cap.textContent || '')) s.style.display = 'none';
    }
  }
  for (const note of document.querySelectorAll('.anno')) note.style.display = 'none'; // the design notes under the footer
}

/**
 * A full-page capture puts a bar that is fixed to the bottom of the window (the phone's bottom navigation) at the
 * height of the window, in the middle of the page. The boards draw it after the content, so the live page is
 * photographed with such a bar laid out at the end of the page.
 */
export function unpinBottomBars() {
  for (const el of document.querySelectorAll('body *')) {
    const cs = getComputedStyle(el);
    if (cs.position !== 'fixed') continue;
    const r = el.getBoundingClientRect();
    if (r.width >= window.innerWidth * 0.9 && Math.abs(r.bottom - window.innerHeight) < 2) { el.style.position = 'static'; document.body.appendChild(el); }
  }
}

/** Makes both renderings quiet: no animation, no caret, no scrollbars, fonts loaded. */
export async function settle() {
  const style = document.createElement('style');
  style.textContent = '*,*::before,*::after{animation:none!important;transition:none!important;caret-color:transparent!important;scroll-behavior:auto!important}::-webkit-scrollbar{display:none!important}html{scrollbar-width:none!important}';
  document.head.appendChild(style);
  if (document.fonts && document.fonts.ready) await document.fonts.ready;
  for (const el of document.querySelectorAll('video,canvas')) el.style.visibility = 'hidden';
  window.scrollTo(0, 0);
}

/** Measures the page: its size and the headings, buttons, cards and chips with their text, box and colours. */
export function measure() {
  const sx = window.scrollX, sy = window.scrollY;
  const norm = (s) => (s || '').replace(/\s+/g, ' ').trim();
  const firstLine = (t) => (t || '').split(/\n/).map(norm).find(Boolean) || '';
  const visible = (e) => {
    const r = e.getBoundingClientRect();
    if (r.width < 2 || r.height < 2) return false;
    for (let n = e; n && n.nodeType === 1; n = n.parentElement) {
      const cs = getComputedStyle(n);
      if (cs.display === 'none' || cs.visibility === 'hidden' || cs.opacity === '0') return false;
      if (n.matches('.sr, .sr-only, [hidden], template, script, style')) return false;
    }
    return true;
  };
  const hex = (c) => {
    const m = /rgba?\(([^)]+)\)/.exec(c || '');
    if (!m) return null;
    const p = m[1].split(/[ ,/]+/).filter(Boolean).map(Number);
    if (p.length >= 4 && p[3] === 0) return null;
    return '#' + p.slice(0, 3).map((v) => Math.round(v).toString(16).padStart(2, '0')).join('');
  };
  const own = (e, kind) => {
    const cs = getComputedStyle(e);
    const r = e.getBoundingClientRect();
    const text = norm(e.innerText || e.value || e.getAttribute('aria-label') || '');
    let key = text;
    if (kind === 'card') {
      const h = e.querySelector('h1,h2,h3,h4,h5,.ui.header');
      key = firstLine(h ? h.innerText : e.innerText).slice(0, 40);
    }
    let box = r;
    if (kind === 'heading') { // a heading is as wide as its text, not as its block
      const range = document.createRange();
      range.selectNodeContents(e);
      const tr = range.getBoundingClientRect();
      if (tr.width > 0) box = {left: tr.left, top: r.top, width: tr.width, height: r.height};
    }
    return {
      kind, key: key.slice(0, 60), text: text.slice(0, 80),
      x: Math.round(box.left + sx), y: Math.round(box.top + sy), w: Math.round(box.width), h: Math.round(box.height),
      color: hex(cs.color), bg: hex(cs.backgroundColor), border: cs.borderTopWidth !== '0px' ? hex(cs.borderTopColor) : null,
      font: cs.fontSize, weight: cs.fontWeight, family: cs.fontFamily.split(',')[0].replace(/["']/g, '').trim(), radius: cs.borderTopLeftRadius,
    };
  };
  const kinds = {
    heading: 'h1,h2,h3,h4',
    button: 'button, a.btn, a.button, .ui.button, input[type=submit], input[type=button], [role=button]',
    card: '.card, .ui.segment, .ui.card, .box, section[class*=card]',
    chip: '.chip, .ui.label, .label, .tag, .badge',
  };
  const elements = [];
  for (const [kind, sel] of Object.entries(kinds)) {
    let n = 0;
    for (const e of document.querySelectorAll(sel)) {
      if (n >= 300) break;
      if (!visible(e)) continue;
      const o = own(e, kind);
      if (!o.key) continue;
      if (kind === 'card' && (o.w < 120 || o.h < 40)) continue;
      elements.push(o);
      n++;
    }
  }
  const root = document.querySelector('.app') || document.body;
  return {
    width: document.documentElement.scrollWidth,
    height: document.documentElement.scrollHeight,
    pageBg: hex(getComputedStyle(root).backgroundColor) || hex(getComputedStyle(document.body).backgroundColor) || hex(getComputedStyle(document.documentElement).backgroundColor),
    pageColor: hex(getComputedStyle(root).color),
    bodyFamily: getComputedStyle(document.body).fontFamily.split(',')[0].replace(/["']/g, '').trim(),
    title: document.title,
    h1: norm((document.querySelector('h1') || {}).innerText),
    elements,
  };
}
