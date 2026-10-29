// The filter of the "Today" feed on the dashboard: a group [data-feed-filter] of buttons with data-kind ("" is everything)
// narrows the items [data-feed-item][data-kind] of the list next to it. The choice lasts for the page only; the number
// shown is announced in the status line [data-feed-count].

export function matches(kind, filter) {
  return filter === '' || kind === filter;
}

export function initAscFeedFilter() {
  for (const group of document.querySelectorAll('[data-feed-filter]')) {
    const list = document.querySelector(group.getAttribute('data-feed-filter'));
    if (!list) continue;
    const status = group.parentElement?.querySelector('[data-feed-count]');
    const apply = (filter) => {
      let shown = 0;
      for (const item of list.querySelectorAll('[data-feed-item]')) {
        const show = matches(item.getAttribute('data-kind') ?? '', filter);
        item.hidden = !show;
        if (show) shown++;
      }
      for (const btn of group.querySelectorAll('button[data-kind]')) {
        btn.setAttribute('aria-pressed', String(btn.getAttribute('data-kind') === filter));
      }
      if (status) status.textContent = (status.getAttribute('data-template') ?? '').replace('%d', String(shown));
    };
    group.addEventListener('click', (e) => {
      const btn = e.target instanceof Element ? e.target.closest('button[data-kind]') : null;
      if (btn) apply(btn.getAttribute('data-kind') ?? '');
    });
  }
}
