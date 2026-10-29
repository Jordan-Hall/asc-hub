// The overlays of the ASC design system shell (templates/base/head_navbar.tmpl, footer_content.tmpl):
// the menu drawer, the account sheet, the create menu and the command palette, plus the footer accordions and
// the language menu. Every trigger is an element with data-ds-open="<overlay id>"; data-ds-close closes the open
// overlay. Esc closes it and the focus goes back to the trigger. On a phone the drawer, the sheets and the
// palette lock the page behind them and keep the Tab key inside; on a desktop the two menus are popovers that
// close on a click outside. Everything the script does is also reachable without it: the drawer opens through
// :target, the search trigger is a link to the explore page.

import {shortcutsEnabled} from './ds-prefs.js';
import {GET} from '../modules/fetch.js';

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

export const paletteScopes = ['all', 'changes', 'code', 'people', 'commands'];
const recentKey = 'asc-palette-recent';
const recentMax = 5;

// The scope after (or before, with back) the current one; the list wraps.
export function cycleScope(current, back = false) {
  const at = Math.max(0, paletteScopes.indexOf(current));
  const next = (at + (back ? paletteScopes.length - 1 : 1)) % paletteScopes.length;
  return paletteScopes[next];
}

// Reads the saved recent items; anything that is not a list of {href, label} with a path on this host is dropped.
export function parseRecents(raw) {
  let list;
  try {
    list = JSON.parse(raw || '[]');
  } catch {
    return [];
  }
  if (!Array.isArray(list)) return [];
  return list.filter((r) => r && typeof r.href === 'string' && typeof r.label === 'string' && /^\/(?!\/)/.test(r.href)).slice(0, recentMax)
    .map((r) => ({href: r.href, label: r.label, sub: typeof r.sub === 'string' ? r.sub : '', kind: r.kind === 'change' ? 'change' : 'repo'}));
}

// Puts an entry first, without a duplicate, and keeps the newest few.
export function pushRecent(list, entry) {
  return [entry, ...list.filter((r) => r.href !== entry.href)].slice(0, recentMax);
}

const notOwners = ['-', 'user', 'org', 'repo', 'admin', 'explore', 'api', 'assets', 'notifications', 'pulls', 'issues', 'milestones'];

// What a page path names: a repository home, or one change, as {kind, href, name, number}; null for any other page.
export function visitFromPath(pathname, appSubUrl = '') {
  let path = pathname;
  if (appSubUrl && path.startsWith(appSubUrl)) path = path.slice(appSubUrl.length);
  const m = /^\/([^/]+)\/([^/]+)(?:\/(pulls|issues)\/(\d+))?\/?$/.exec(path);
  if (!m || notOwners.includes(m[1])) return null;
  if (m[3]) return {kind: 'change', href: `${appSubUrl}/${m[1]}/${m[2]}/${m[3]}/${m[4]}`, name: `${m[1]}/${m[2]}`, number: m[4]};
  return {kind: 'repo', href: `${appSubUrl}/${m[1]}/${m[2]}`, name: `${m[1]}/${m[2]}`, number: ''};
}

function recordVisit() {
  if (!document.querySelector('.page-content.repository')) return;
  const visit = visitFromPath(window.location.pathname, window.config?.appSubUrl || '');
  if (!visit) return;
  const title = document.querySelector('#issue-title-display, .issue-title h1')?.textContent.trim().replace(/\s+#\d+$/, '') || `#${visit.number}`;
  const entry = visit.kind === 'change' ?
    {href: visit.href, kind: 'change', label: title, sub: `${visit.name} #${visit.number}`} :
    {href: visit.href, kind: 'repo', label: visit.name, sub: ''};
  try {
    localStorage.setItem(recentKey, JSON.stringify(pushRecent(parseRecents(localStorage.getItem(recentKey)), entry)));
  } catch {
    // storage is blocked: the palette just shows no recent items
  }
}

let scope = 'all';
let itemSeq = 0;

function paletteParts() {
  const root = byId('ds-palette');
  if (!root) return null;
  return {
    root,
    input: root.querySelector('input[data-asc-search]'),
    list: root.querySelector('[data-ds-palette-list]'),
    dynamic: root.querySelector('[data-ds-palette-dynamic]'),
    recent: root.querySelector('[data-ds-palette-recent]'),
    empty: root.querySelector('[data-ds-palette-empty]'),
    form: root.querySelector('form'),
  };
}

function visibleItems(p) {
  return Array.from(p.list.querySelectorAll('.ds-palette-item')).filter((el) => !el.hidden);
}

function select(p, index) {
  const items = visibleItems(p);
  for (const it of items) {
    it.classList.remove('is-sel');
    it.setAttribute('aria-selected', 'false');
  }
  if (index < 0 || index >= items.length) {
    p.input?.setAttribute('aria-activedescendant', '');
    return;
  }
  items[index].classList.add('is-sel');
  items[index].setAttribute('aria-selected', 'true');
  p.input?.setAttribute('aria-activedescendant', items[index].id);
  items[index].scrollIntoView({block: 'nearest'});
}

// every option of the listbox has an id (aria-activedescendant points at it) and the option role
function prepareItem(el) {
  if (!el.id) el.id = `ds-pi-${++itemSeq}`;
  el.setAttribute('role', 'option');
  if (!el.hasAttribute('aria-selected')) el.setAttribute('aria-selected', 'false');
}

function dynamicItem(href, label, query, itemScope) {
  const a = document.createElement('a');
  a.className = 'ds-palette-item';
  a.href = href;
  a.dataset.dsScope = itemScope;
  const main = document.createElement('span');
  main.className = 'ds-pi-main';
  main.textContent = `${label} `;
  const strong = document.createElement('strong');
  strong.textContent = `“${query}”`;
  main.append(strong);
  a.append(searchIcon(), main);
  prepareItem(a);
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

// the recent repositories and changes of this browser, as options of the listbox (empty query only)
function renderRecents(p) {
  p.recent.replaceChildren();
  let list = [];
  try {
    list = parseRecents(localStorage.getItem(recentKey));
  } catch {
    // no storage
  }
  const icons = p.root.querySelector('template[data-ds-recent-icons]')?.content.querySelectorAll('svg') || [];
  for (const r of list) {
    const a = document.createElement('a');
    a.className = 'ds-palette-item';
    a.href = r.href;
    a.dataset.dsScope = 'all';
    a.dataset.dsWhen = 'empty';
    const icon = icons[r.kind === 'change' ? 1 : 0];
    if (icon) a.append(icon.cloneNode(true));
    const main = document.createElement('span');
    main.className = 'ds-pi-main';
    main.textContent = r.label;
    if (r.sub) {
      const small = document.createElement('small');
      small.textContent = r.sub;
      main.append(small);
    }
    a.append(main);
    prepareItem(a);
    p.recent.append(a);
  }
}

function setScope(p, next) {
  scope = next;
  for (const chip of p.root.querySelectorAll('[data-ds-scope][aria-pressed]')) {
    chip.setAttribute('aria-pressed', String(chip.dataset.dsScope === scope));
  }
  filterPalette();
}

function filterPalette() {
  const p = paletteParts();
  if (!p || !p.input) return;
  const q = p.input.value.trim();
  const needle = q.toLowerCase();
  const searching = q !== '';
  // what to search for (typed text only)
  p.dynamic.replaceChildren();
  if (searching) {
    const d = p.root.dataset;
    const enc = encodeURIComponent(q);
    const hasAsc = d.ascSearchUrl !== d.exploreReposUrl;
    p.dynamic.append(dynamicItem(`${d.ascSearchUrl}?q=${enc}`, hasAsc ? d.tCode : d.tRepos, q, 'code'));
    if (hasAsc) p.dynamic.append(dynamicItem(`${d.exploreReposUrl}?q=${enc}`, d.tRepos, q, 'code'));
    p.dynamic.append(dynamicItem(`${d.exploreUsersUrl}?q=${enc}`, d.tPeople, q, 'people'));
  }
  for (const el of p.dynamic.children) el.hidden = scope !== 'all' && el.dataset.dsScope !== scope;
  renderRecents(p);
  const emptyView = !searching && scope === 'all';
  // the destinations and commands, the recent items and the shortcuts
  let group = null;
  let groupVisible = 0;
  const flush = () => {
    if (group) group.hidden = groupVisible === 0;
  };
  for (const node of Array.from(p.list.children)) {
    if (node === p.dynamic) continue;
    const nodes = node === p.recent ? Array.from(p.recent.children) : [node];
    for (const n of nodes) {
      if (n.hasAttribute('data-ds-group')) {
        flush();
        group = n;
        groupVisible = 0;
      } else if (n.classList.contains('ds-palette-item')) {
        prepareItem(n);
        const text = (n.getAttribute('data-ds-text') || n.textContent).toLowerCase();
        const hit = n.dataset.dsWhen === 'empty' ? emptyView : (scope === 'all' || n.dataset.dsScope === scope) && (needle === '' || text.includes(needle));
        n.hidden = !hit;
        if (hit) groupVisible++;
      }
    }
  }
  flush();
  const any = visibleItems(p).length > 0;
  if (p.empty) {
    p.empty.hidden = any;
    const title = p.empty.querySelector('[data-ds-palette-empty-title]');
    if (title) title.textContent = (title.dataset.tTitle || '').replace('%s', q);
  }
  select(p, any && searching ? 0 : -1);
}

function resetPalette() {
  const p = paletteParts();
  if (!p || !p.input) return;
  p.input.value = '';
  setScope(p, 'all');
}

function openShortcuts() {
  const dlg = byId('ds-shortcuts');
  if (!dlg || typeof dlg.showModal !== 'function' || dlg.open) return;
  closeOverlay({restoreFocus: false});
  dlg.showModal();
}

function onPaletteKeydown(e) {
  if (openId !== 'ds-palette') return;
  const p = paletteParts();
  if (!p) return;
  if (e.key === 'Tab' && !e.ctrlKey && !e.metaKey && !e.altKey && e.target === p.input) {
    // Tab cycles the scope chip; Escape and the Cancel button leave the palette
    e.preventDefault();
    e.stopImmediatePropagation();
    setScope(p, cycleScope(scope, e.shiftKey));
    return;
  }
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
    return;
  }
  if (e.key === '?' && !e.ctrlKey && !e.metaKey && !isTyping(e.target) && shortcutsEnabled() && byId('ds-shortcuts')) {
    e.preventDefault();
    openShortcuts();
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
  if (p) {
    p.root.addEventListener('click', (e) => {
      const target = e.target instanceof Element ? e.target : null;
      const chip = target?.closest('[data-ds-scope][aria-pressed]');
      if (chip) {
        setScope(p, chip.dataset.dsScope);
        p.input.focus();
      } else if (target?.closest('[data-ds-shortcuts]')) {
        openShortcuts();
      }
    });
    filterPalette();
  }
  recordVisit();
  initFooterGroups();
  initLangMenu();
}

// ---- live results of the palette: changes, people and repositories that match what is typed ----

const liveMinLength = 2;
const liveDelay = 180;
let liveTimer = 0;
let liveAbort = null;

// groupScope says in which scope chip a group of the live results shows
export const liveGroupScopes = {changes: 'changes', people: 'people', repos: 'all'};

export function liveQuery(raw) {
  const q = String(raw ?? '').trim();
  return q.length >= liveMinLength && q.length <= 80 ? q : '';
}

function liveContainer(p) {
  let box = p.list.querySelector('[data-ds-palette-live]');
  if (!box) {
    box = document.createElement('div');
    box.setAttribute('data-ds-palette-live', '');
    box.setAttribute('role', 'presentation');
    p.dynamic.after(box);
  }
  return box;
}

export function renderLive(p, groups) {
  const box = liveContainer(p);
  box.replaceChildren();
  for (const g of groups ?? []) {
    if (!Array.isArray(g.items) || g.items.length === 0 || !(g.key in liveGroupScopes)) continue;
    const head = document.createElement('div');
    head.className = 'ds-palette-group';
    head.setAttribute('role', 'presentation');
    head.dataset.dsScope = liveGroupScopes[g.key];
    head.textContent = g.label ?? '';
    box.append(head);
    for (const it of g.items) {
      // only paths of this site: a result never leads away from the hub
      if (typeof it.href !== 'string' || !it.href.startsWith('/') || it.href.startsWith('//')) continue;
      const a = document.createElement('a');
      a.className = 'ds-palette-item';
      a.href = it.href;
      a.dataset.dsScope = liveGroupScopes[g.key];
      const main = document.createElement('span');
      main.className = 'ds-pi-main';
      main.textContent = it.title ?? '';
      if (it.sub) {
        const small = document.createElement('small');
        small.textContent = it.sub;
        main.append(small);
      }
      a.append(main);
      prepareItem(a);
      box.append(a);
    }
  }
  for (const el of box.children) el.hidden = scope !== 'all' && el.dataset.dsScope !== scope;
}

function scheduleLive() {
  const p = paletteParts();
  if (!p || !p.input || !p.root.dataset.liveUrl) return;
  clearTimeout(liveTimer);
  liveAbort?.abort();
  const q = liveQuery(p.input.value);
  if (!q) {
    liveContainer(p).replaceChildren();
    return;
  }
  liveTimer = setTimeout(async () => {
    liveAbort = new AbortController();
    try {
      const res = await GET(`${p.root.dataset.liveUrl}?q=${encodeURIComponent(q)}`, {signal: liveAbort.signal});
      if (!res.ok || liveQuery(p.input.value) !== q) return;
      renderLive(p, (await res.json()).groups);
      const any = visibleItems(p).length > 0;
      if (p.empty) p.empty.hidden = any;
      if (any && !p.list.querySelector('.is-sel')) select(p, 0);
    } catch {
      // aborted or offline: the static results stay
    }
  }, liveDelay);
}

document.addEventListener('input', (e) => {
  if (e.target instanceof HTMLInputElement && e.target.matches('#ds-palette input[data-asc-search]')) scheduleLive();
});
