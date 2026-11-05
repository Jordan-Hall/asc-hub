const openMenus = (doc) => doc.querySelectorAll('details[data-asc-row-menu][open]');

/**
 * The row menus of a repository list on a phone (templates/explore/repo_list.tmpl, board Mobile-OrgProfile): one menu
 * is open at a time, and a tap outside, Escape or an item of the menu closes it. Escape gives the focus back to its
 * button.
 */
export function initAscRowMenus(doc = document) {
  if (!doc.querySelector('details[data-asc-row-menu]')) return;
  doc.addEventListener('toggle', (e) => {
    const menu = e.target;
    if (!menu.matches?.('details[data-asc-row-menu]') || !menu.open) return;
    for (const other of openMenus(doc)) {
      if (other !== menu) other.open = false;
    }
  }, true);
  doc.addEventListener('click', (e) => {
    const inside = e.target.closest?.('details[data-asc-row-menu]');
    const item = e.target.closest?.('.wk-menu-item');
    for (const menu of openMenus(doc)) {
      if (menu !== inside || item) menu.open = false;
    }
  });
  doc.addEventListener('keydown', (e) => {
    if (e.key !== 'Escape') return;
    const menu = e.target.closest?.('details[data-asc-row-menu][open]');
    if (!menu) return;
    menu.open = false;
    menu.querySelector('summary').focus();
  });
}
