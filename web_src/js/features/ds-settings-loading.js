const skeletonFields = 4;

/** Puts skeleton inputs in place of a settings page while the next one loads, and says to screen readers that it is loading. */
export function showSettingsLoading(body) {
  if (body.classList.contains('is-loading')) return;
  body.classList.add('is-loading');
  body.setAttribute('aria-busy', 'true');
  const sk = document.createElement('div');
  sk.className = 'ds-settings-sk';
  const head = document.createElement('div');
  head.className = 'ds-settings-sk-head';
  head.setAttribute('aria-hidden', 'true');
  for (const part of ['ds-settings-sk-title', 'ds-settings-sk-lead']) {
    const bar = document.createElement('span');
    bar.className = `ds-sk ${part}`;
    head.append(bar);
  }
  sk.append(head);
  for (let i = 0; i < skeletonFields; i++) {
    const field = document.createElement('div');
    field.className = 'ds-settings-sk-field';
    field.setAttribute('aria-hidden', 'true');
    for (const part of ['ds-settings-sk-label', 'ds-settings-sk-input']) {
      const bar = document.createElement('span');
      bar.className = `ds-sk ${part}`;
      field.append(bar);
    }
    sk.append(field);
  }
  const status = document.createElement('div');
  status.className = 'ds-sr';
  status.setAttribute('role', 'status');
  status.textContent = body.getAttribute('data-loading') ?? '';
  sk.append(status);
  body.append(sk);
}

/** Gives the page its content back, for a page restored from the back-forward cache. */
export function hideSettingsLoading(body) {
  body.classList.remove('is-loading');
  body.removeAttribute('aria-busy');
  for (const sk of body.querySelectorAll('.ds-settings-sk')) sk.remove();
}

/** Whether following a link of the settings menu loads another settings page: same origin, other path, plain click. */
export function loadsOtherPage(link, e, here = window.location) {
  if (e.defaultPrevented || e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return false;
  if (link.target && link.target !== '_self') return false;
  if (link.hasAttribute('download')) return false;
  const to = new URL(link.href, here.href);
  return to.origin === here.origin && to.pathname !== here.pathname;
}

/**
 * The settings pages (account, organization, repository, site administration): a link of the left menu loads another
 * page, and until it does the content shows skeleton inputs (board UserSettingsProfile, "Loading: fields render as
 * skeleton inputs").
 */
export function initSettingsLoading(doc = document) {
  const body = doc.querySelector('.ds-settings-body[data-loading]');
  const nav = doc.querySelector('.ds-settings-nav');
  if (!body || !nav) return;
  nav.addEventListener('click', (e) => {
    const link = e.target.closest?.('a[href]');
    if (link && loadsOtherPage(link, e)) showSettingsLoading(body);
  });
  window.addEventListener('pageshow', (e) => {
    if (e.persisted) hideSettingsLoading(body);
  });
}
