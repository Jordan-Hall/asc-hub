// The changes of a release card (board Releases): the chips All, Added, Changed and Fixed show the changes of one
// category. Without scripts every change is shown and the chips do nothing.

export function showCategory(list, category) {
  for (const item of list.querySelectorAll('li[data-rel-cat]')) {
    item.hidden = Boolean(category) && item.getAttribute('data-rel-cat') !== category;
  }
}

function onFilterClick(e) {
  const chip = e.target.closest('button[data-rel-cat]');
  if (!chip) return;
  const box = chip.closest('[data-rel-changes]');
  for (const other of box.querySelectorAll('button[data-rel-cat]')) {
    other.setAttribute('aria-pressed', String(other === chip));
  }
  showCategory(box.querySelector('.rp-rel-list'), chip.getAttribute('data-rel-cat'));
}

export function initAscReleaseChanges() {
  for (const box of document.querySelectorAll('[data-rel-changes]')) {
    const filter = box.querySelector('.rp-rel-filter');
    if (filter) filter.addEventListener('click', onFilterClick);
  }
}
