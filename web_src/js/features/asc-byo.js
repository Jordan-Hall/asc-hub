import {GET} from '../modules/fetch.js';

const pollMs = 2000;

// Keeps the progress of a storage migration current on the repository storage settings page. The element
// carries the URL of the status endpoint; the answer is only numbers and the translated state label, which
// are written with textContent. When the migration is no longer active the page reloads once, so the
// history table and the forms reflect the new state.
export function initAscByoMigration() {
  const box = document.querySelector('[data-asc-migration-status-url]');
  if (!box) return;
  const url = box.getAttribute('data-asc-migration-status-url');
  const label = box.querySelector('.asc-migration-label');
  const bar = box.querySelector('.asc-migration-bar');
  const count = box.querySelector('.asc-migration-count');

  const refresh = async () => {
    const res = await GET(url, {credentials: 'same-origin', cache: 'no-store'});
    if (res.url.endsWith('/login')) return false; // signed out: stop
    if (res.status === 404) return false; // gone or not ours: stop
    if (res.status !== 200) return true; // try again later
    const data = await res.json();
    if (label && typeof data.label === 'string') label.textContent = data.label;
    if (bar && Number.isFinite(data.percent)) {
      bar.value = data.percent;
      bar.textContent = `${data.percent}%`;
    }
    if (count && Number.isFinite(data.copied) && Number.isFinite(data.total)) {
      count.textContent = `${data.copied} / ${data.total}`;
    }
    if (data.active === false) {
      window.location.reload();
      return false;
    }
    return true;
  };

  const loop = async () => {
    let again = true;
    try {
      again = await refresh();
    } catch {
      again = true; // network error: keep trying
    } finally {
      if (again) setTimeout(loop, pollMs);
    }
  };
  setTimeout(loop, pollMs);
}
