// The settings menu (boards UserSettingsMenu, RepoSettingsMenu). "Find a setting" on its phone page: the field with
// data-asc-snav-find hides the entries of the menu (and the groups left without one) whose name and description
// (data-snav-desc, not shown) do not contain what was typed, and shows data-asc-snav-none when nothing is left. The
// text is compared without case and without accents. The phone page keeps its scroll: tapping an entry remembers
// it, and the back arrow (#settings-menu) brings the menu back where it was. A menu with data-snav-live-url fills
// its data-snav-live statuses from that JSON when the page has loaded.

import {GET} from '../modules/fetch.js';

const fold = (text) => text.normalize('NFD').replace(/\p{Diacritic}/gu, '').toLowerCase().trim();

export function matchSetting(label, query) {
  const q = fold(query);
  return q === '' || fold(label).includes(q);
}

// The words an entry is found by: its name and its description.
export function entryText(entry) {
  const link = entry.matches('a') ? entry : entry.querySelector('a');
  return `${entry.textContent || ''} ${link?.getAttribute('data-snav-desc') || ''}`;
}

export function filterMenu(menu, query) {
  let shown = 0;
  for (const group of menu.querySelectorAll('.ds-settings-group')) {
    const list = group.nextElementSibling;
    if (!list || !list.matches('.ds-settings-list')) continue;
    let any = 0;
    for (const item of list.querySelectorAll('li')) {
      const hit = matchSetting(entryText(item), query);
      item.hidden = !hit;
      if (hit) any++;
    }
    group.hidden = list.hidden = any === 0;
    shown += any;
  }
  const danger = menu.querySelector('.ds-snav-danger');
  if (danger) {
    danger.hidden = !matchSetting(entryText(danger), query);
    if (!danger.hidden) shown++;
  }
  menu.querySelector('[data-asc-snav-none]')?.classList.toggle('ds-hidden', shown > 0);
  return shown;
}

const scrollKey = (nav) => `asc-snav-scroll:${nav.getAttribute('aria-label') || ''}`;

export function keepMenuScroll(nav) {
  nav.addEventListener('click', (e) => {
    if (e.target.closest('a[href]') && !e.target.closest('.ds-settings-bar')) {
      sessionStorage.setItem(scrollKey(nav), String(nav.scrollTop));
    }
  });
  const restore = () => {
    if (window.location.hash !== `#${nav.id}`) return;
    const top = Number(sessionStorage.getItem(scrollKey(nav)));
    if (top > 0) nav.scrollTop = top;
  };
  window.addEventListener('hashchange', restore);
  restore();
}

// Fills the data-snav-live statuses of nav from the JSON at its data-snav-live-url. A status the answer leaves out
// stays hidden. Returns the number filled.
export async function fillMenuStatus(nav) {
  const url = nav.getAttribute('data-snav-live-url');
  if (!url) return 0;
  let data;
  try {
    const res = await GET(url);
    if (!res.ok) return 0;
    data = await res.json();
  } catch {
    return 0;
  }
  let filled = 0;
  for (const span of nav.querySelectorAll('[data-snav-live]')) {
    const text = data?.[span.getAttribute('data-snav-live')];
    if (typeof text !== 'string' || text === '') continue;
    span.textContent = text;
    span.hidden = false;
    filled++;
  }
  return filled;
}

export function initAscSettingsFind() {
  const nav = document.querySelector('.ds-settings-nav');
  if (!nav) return;
  keepMenuScroll(nav);
  fillMenuStatus(nav);
  const input = nav.querySelector('[data-asc-snav-find]');
  const menu = input?.closest('.ds-settings-nav-in');
  if (!menu) return;
  input.addEventListener('input', () => filterMenu(menu, input.value));
}
