// "Find a setting" on the phone menu of the settings: the field with data-asc-snav-find hides the entries of the
// menu (and the groups left without one) that do not contain what was typed, and shows data-asc-snav-none when
// nothing is left. The text is compared without case and without accents.

const fold = (text) => text.normalize('NFD').replace(/[̀-ͯ]/g, '').toLowerCase().trim();

export function matchSetting(label, query) {
  const q = fold(query);
  return q === '' || fold(label).includes(q);
}

export function initAscSettingsFind() {
  const input = document.querySelector('[data-asc-snav-find]');
  const menu = input?.closest('.ds-settings-nav-in');
  if (!menu) return;
  const none = menu.querySelector('[data-asc-snav-none]');
  input.addEventListener('input', () => {
    let shown = 0;
    for (const group of menu.querySelectorAll('.ds-settings-group')) {
      const list = group.nextElementSibling;
      if (!list || !list.matches('.ds-settings-list')) continue;
      let any = 0;
      for (const item of list.querySelectorAll('li')) {
        const hit = matchSetting(item.textContent || '', input.value);
        item.hidden = !hit;
        if (hit) any++;
      }
      group.hidden = list.hidden = any === 0;
      shown += any;
    }
    none?.classList.toggle('ds-hidden', shown > 0);
  });
}
