// The overlays of the ASC design system shell (templates/base/head_navbar.tmpl, footer_content.tmpl):
// the menu drawer, the account sheet, the create menu and the command palette, plus the footer accordions and
// the language menu. Every trigger is an element with data-ds-open="<overlay id>"; data-ds-close closes the open
// overlay. Esc closes it and the focus goes back to the trigger. On a phone the drawer, the sheets and the
// palette lock the page behind them and keep the Tab key inside; on a desktop the two menus are popovers that
// close on a click outside. Everything the script does is also reachable without it: the drawer opens through
// :target, the search trigger is a link to the explore page.

const desktopQuery = '(min-width: 1024px)';
const footerQuery = '(max-width: 767.98px)';

let openId = null;
let lastTrigger = null;

function byId(id) {
  return document.getElementById(id);
}

function isDesktop() {
  return window.matchMedia(desktopQuery).matches;
}

// The overlays that block the page behind them: the drawer and the palette always, the two menus on a phone.
function isModal(id) {
  if (id === 'ds-drawer' || id === 'ds-palette') return true;
  return !isDesktop();
}

function focusables(root) {
  return Array.from(root.querySelectorAll('a[href], button:not([disabled]), input:not([disabled]), select, textarea, [tabindex]:not([tabindex="-1"])'))
    .filter((el) => el.offsetParent !== null || el === document.activeElement);
}

function setExpanded(id, value) {
  for (const t of document.querySelectorAll(`[data-ds-open="${id}"]`)) {
    if (t.hasAttribute('aria-expanded')) t.setAttribute('aria-expanded', String(value));
  }
}

export function closeOverlay({restoreFocus = true} = {}) {
  if (!openId) return;
  const id = openId;
  const el = byId(id);
  openId = null;
  if (el) el.classList.remove('is-open');
  byId('ds-scrim')?.classList.remove('is-open');
  document.body.classList.remove('ds-lock');
  setExpanded(id, false);
  if (id === 'ds-palette') resetPalette();
  if (window.location.hash === `#${id}`) window.history.replaceState(null, '', window.location.pathname + window.location.search);
  const trigger = lastTrigger;
  lastTrigger = null;
  if (restoreFocus && trigger && document.contains(trigger)) trigger.focus();
}

export function openOverlay(id, trigger) {
  const el = byId(id);
  if (!el) return false;
  if (openId === id) {
    closeOverlay();
    return true;
  }
  closeOverlay({restoreFocus: false});
  openId = id;
  lastTrigger = trigger || document.activeElement;
  el.classList.add('is-open');
  setExpanded(id, true);
  if (isModal(id)) {
    byId('ds-scrim')?.classList.toggle('is-open', id !== 'ds-palette');
    document.body.classList.add('ds-lock');
  }
  if (id === 'ds-palette') {
    const input = el.querySelector('input[data-asc-search]');
    input?.focus();
  } else {
    const first = focusables(el)[0];
    first?.focus();
  }
  return true;
}

function onClick(e) {
  const target = e.target instanceof Element ? e.target : null;
  if (!target) return;
  const opener = target.closest('[data-ds-open]');
  if (opener) {
    e.preventDefault();
    openOverlay(opener.getAttribute('data-ds-open'), opener);
    return;
  }
  const closer = target.closest('[data-ds-close]');
  if (closer && openId) {
    if (closer.tagName === 'A') e.preventDefault();
    closeOverlay();
    return;
  }
  if (!openId) return;
  const el = byId(openId);
  // a click on a link inside an overlay leaves the page (or the hash): close it first so it never stays open
  const link = target.closest('a[href]');
  if (el && link && el.contains(link) && !link.hasAttribute('data-ds-keep')) {
    closeOverlay({restoreFocus: false});
    return;
  }
  // a popover closes on a click outside of it
  if (!isModal(openId) && el && !el.contains(target)) closeOverlay({restoreFocus: false});
}

function onKeydown(e) {
  if (e.key === 'Escape' && openId) {
    e.preventDefault();
    closeOverlay();
    return;
  }
  if (e.key === 'Tab' && openId && isModal(openId)) {
    const el = byId(openId);
    const list = el ? focusables(el) : [];
    if (!list.length) return;
    const first = list[0];
    const last = list[list.length - 1];
    if (e.shiftKey && document.activeElement === first) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && document.activeElement === last) {
      e.preventDefault();
      first.focus();
    }
  }
}

// ---- the command palette ----

function paletteParts() {
  const root = byId('ds-palette');
  if (!root) return null;
  return {
    root,
    input: root.querySelector('input[data-asc-search]'),
    list: root.querySelector('[data-ds-palette-list]'),
    dynamic: root.querySelector('[data-ds-palette-dynamic]'),
    empty: root.querySelector('[data-ds-palette-empty]'),
    form: root.querySelector('form'),
  };
}

function visibleItems(p) {
  return Array.from(p.list.querySelectorAll('.ds-palette-item')).filter((el) => !el.hidden);
}

function select(p, index) {
  const items = visibleItems(p);
  for (const it of items) it.classList.remove('is-sel');
  if (index < 0 || index >= items.length) return;
  items[index].classList.add('is-sel');
  items[index].scrollIntoView({block: 'nearest'});
}

function dynamicItem(href, label, query) {
  const a = document.createElement('a');
  a.className = 'ds-palette-item';
  a.href = href;
  const main = document.createElement('span');
  main.className = 'ds-pi-main';
  main.textContent = `${label} `;
  const strong = document.createElement('strong');
  strong.textContent = `“${query}”`;
  main.append(strong);
  a.append(searchIcon(), main);
  return a;
}

const svgNS = 'http://www.w3.org/2000/svg';

// the search glyph of the board icon set, built with the DOM so no markup string is parsed
function searchIcon() {
  const svg = document.createElementNS(svgNS, 'svg');
  for (const [k, v] of Object.entries({viewBox: '0 0 24 24', width: '17', height: '17', fill: 'none', stroke: 'currentColor', 'stroke-width': '1.8', 'stroke-linecap': 'round', 'stroke-linejoin': 'round', 'aria-hidden': 'true'})) svg.setAttribute(k, v);
  const c = document.createElementNS(svgNS, 'circle');
  c.setAttribute('cx', '11');
  c.setAttribute('cy', '11');
  c.setAttribute('r', '6.5');
  const path = document.createElementNS(svgNS, 'path');
  path.setAttribute('d', 'M16 16l4.5 4.5');
  svg.append(c, path);
  return svg;
}

function filterPalette() {
  const p = paletteParts();
  if (!p || !p.input) return;
  const q = p.input.value.trim();
  const needle = q.toLowerCase();
  // the static destinations and commands
  let group = null;
  let groupVisible = 0;
  const flush = () => {
    if (group) group.hidden = groupVisible === 0;
  };
  for (const node of Array.from(p.list.children)) {
    if (node === p.dynamic) continue;
    if (node.hasAttribute('data-ds-group')) {
      flush();
      group = node;
      groupVisible = 0;
    } else if (node.classList.contains('ds-palette-item')) {
      const text = (node.getAttribute('data-ds-text') || node.textContent).toLowerCase();
      const hit = needle === '' || text.includes(needle);
      node.hidden = !hit;
      if (hit) groupVisible++;
    }
  }
  flush();
  // what to search for
  p.dynamic.replaceChildren();
  if (q !== '') {
    const d = p.root.dataset;
    const enc = encodeURIComponent(q);
    const hasAsc = d.ascSearchUrl !== d.exploreReposUrl;
    p.dynamic.append(dynamicItem(`${d.ascSearchUrl}?q=${enc}`, hasAsc ? d.tCode : d.tRepos, q));
    if (hasAsc) p.dynamic.append(dynamicItem(`${d.exploreReposUrl}?q=${enc}`, d.tRepos, q));
    p.dynamic.append(dynamicItem(`${d.exploreUsersUrl}?q=${enc}`, d.tPeople, q));
  }
  const any = visibleItems(p).length > 0;
  if (p.empty) p.empty.hidden = any;
  select(p, -1);
}

function resetPalette() {
  const p = paletteParts();
  if (!p || !p.input) return;
  p.input.value = '';
  filterPalette();
}

function onPaletteKeydown(e) {
  if (openId !== 'ds-palette') return;
  const p = paletteParts();
  if (!p) return;
  if (e.key !== 'ArrowDown' && e.key !== 'ArrowUp' && e.key !== 'Enter') return;
  const items = visibleItems(p);
  const cur = items.findIndex((el) => el.classList.contains('is-sel'));
  if (e.key === 'ArrowDown') {
    e.preventDefault();
    select(p, Math.min(items.length - 1, cur + 1));
  } else if (e.key === 'ArrowUp') {
    e.preventDefault();
    select(p, Math.max(0, cur - 1));
  } else if (cur >= 0) {
    e.preventDefault();
    items[cur].click();
  }
}

function isTyping(el) {
  if (!el || !el.tagName) return false;
  const tag = el.tagName.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || el.isContentEditable === true;
}

function onGlobalKey(e) {
  if (e.defaultPrevented || e.altKey) return;
  const isCtrlK = (e.ctrlKey || e.metaKey) && !e.shiftKey && e.key.toLowerCase() === 'k';
  const isSlash = e.key === '/' && !e.ctrlKey && !e.metaKey && !isTyping(e.target);
  if ((isCtrlK || isSlash) && byId('ds-palette')) {
    e.preventDefault();
    if (openId !== 'ds-palette') openOverlay('ds-palette', document.activeElement);
  }
}

// ---- footer groups and language menu ----

function initFooterGroups() {
  const groups = Array.from(document.querySelectorAll('details.ds-footer-group'));
  if (!groups.length) return;
  const mq = window.matchMedia(footerQuery);
  const apply = () => {
    for (const g of groups) g.open = !mq.matches;
  };
  apply();
  mq.addEventListener('change', apply);
}

function initLangMenu() {
  const lang = document.querySelector('details.ds-lang');
  if (!lang) return;
  document.addEventListener('click', (e) => {
    if (lang.open && e.target instanceof Node && !lang.contains(e.target)) lang.open = false;
  });
  lang.addEventListener('keydown', (e) => {
    if (e.key === 'Escape' && lang.open) {
      lang.open = false;
      lang.querySelector('summary')?.focus();
    }
  });
}

// Show or hide a password: <button data-ds-reveal="<input id>" data-show="Show" data-hide="Hide">
function onRevealClick(e) {
  const btn = e.target instanceof Element ? e.target.closest('[data-ds-reveal]') : null;
  if (!btn) return;
  const input = document.getElementById(btn.getAttribute('data-ds-reveal'));
  if (!input) return;
  const show = input.type === 'password';
  input.type = show ? 'text' : 'password';
  btn.setAttribute('aria-pressed', String(show));
  btn.textContent = btn.getAttribute(show ? 'data-hide' : 'data-show');
  input.focus();
}

let bound = false;

export function initDsShell() {
  if (!bound) {
    bound = true;
    document.addEventListener('click', onClick);
    document.addEventListener('click', onRevealClick);
    document.addEventListener('keydown', onKeydown);
    document.addEventListener('keydown', onPaletteKeydown);
    document.addEventListener('keydown', onGlobalKey);
    window.matchMedia(desktopQuery).addEventListener('change', () => closeOverlay({restoreFocus: false}));
  }
  const p = paletteParts();
  if (p?.input) p.input.addEventListener('input', filterPalette);
  initFooterGroups();
  initLangMenu();
}
