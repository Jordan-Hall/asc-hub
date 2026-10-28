// Select several rows of a list and act on them (inbox). Markup:
//   <div data-bulk>
//     <input type="checkbox" data-bulk-item value="<id>"> ... one per row
//     <div data-bulk-bar hidden> <span data-bulk-count data-one="1 selected" data-n="%d selected"></span>
//       <button data-bulk-do data-bulk-url="/post/here" data-bulk-field="pull" data-bulk-fields='{"do":"snooze","for":"1h"}'>...</button>
//       <button data-bulk-clear>...</button> </div>
//   </div>
// The bar is a polite live region that says how many rows are chosen. A bulk action sends one request per row to
// the endpoint that already handles one row (same form fields, the page's CSRF token), then reloads the page.
// Every row action keeps working without this script.

import {shortcutsEnabled} from './ds-prefs.js';
import {POST} from '../modules/fetch.js';

export function selectionLabel(count, one, many) {
  return count === 1 ? one : many.replace('%d', String(count));
}

const bulkItems = (root) => Array.from(root.querySelectorAll('input[data-bulk-item]'));
const bulkChosen = (root) => bulkItems(root).filter((el) => el.checked);

function updateBar(root, bar, note) {
  const n = bulkChosen(root).length;
  bar.hidden = n === 0;
  note.textContent = n === 0 ? '' : selectionLabel(n, note.getAttribute('data-one') || '', note.getAttribute('data-n') || '');
  for (const el of bulkItems(root)) el.closest('[data-bulk-row]')?.classList.toggle('is-checked', el.checked);
}

export function initBulk() {
  for (const root of document.querySelectorAll('[data-bulk]')) {
    const bar = root.querySelector('[data-bulk-bar]');
    const note = root.querySelector('[data-bulk-count]');
    if (!bar || !note) continue;
    const items = () => bulkItems(root);
    const chosen = () => bulkChosen(root);
    const update = () => updateBar(root, bar, note);
    root.addEventListener('change', (e) => {
      if (e.target instanceof Element && e.target.matches('input[data-bulk-item]')) update();
    });
    root.addEventListener('keydown', (e) => {
      // x toggles the row that has the focus, when shortcuts are not in a field
      if (e.key !== 'x' || e.ctrlKey || e.metaKey || e.altKey || !shortcutsEnabled()) return;
      const row = e.target instanceof Element ? e.target.closest('[data-bulk-row]') : null;
      const box = row?.querySelector('input[data-bulk-item]');
      if (!box) return;
      box.checked = !box.checked;
      update();
    });
    root.querySelector('[data-bulk-clear]')?.addEventListener('click', () => {
      for (const el of items()) el.checked = false;
      update();
    });
    for (const btn of root.querySelectorAll('[data-bulk-do]')) {
      btn.addEventListener('click', async () => {
        const token = document.querySelector('input[name="_csrf"]')?.value || '';
        const url = btn.getAttribute('data-bulk-url');
        const field = btn.getAttribute('data-bulk-field') || 'id';
        let extra = {};
        try {
          extra = JSON.parse(btn.getAttribute('data-bulk-fields') || '{}');
        } catch {
          extra = {};
        }
        btn.disabled = true;
        let failed = 0;
        for (const el of chosen()) {
          const body = new URLSearchParams({_csrf: token, [field]: el.value, ...extra});
          try {
            const res = await POST(url, {data: body, credentials: 'same-origin'});
            if (!res.ok) failed++;
          } catch {
            failed++;
          }
        }
        if (failed === 0) {
          window.location.reload();
        } else {
          btn.disabled = false;
          note.textContent = btn.getAttribute('data-failed') || '';
        }
      });
    }
    update();
  }
}
