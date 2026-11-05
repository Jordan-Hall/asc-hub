import {POST} from '../modules/fetch.js';

const prefix = 'asc-docs-feedback:';

function safe(fn) {
  try {
    return fn();
  } catch {
    // storage is blocked: the answer is simply not remembered
    return null;
  }
}

/**
 * "Was this page helpful?" on a docs page (templates/asc/docs_page.tmpl, board Docs). Yes or No is sent without leaving
 * the page; the chosen button is pressed, both are disabled, and the status line thanks the reader. The answer is
 * remembered on this device, so the page does not ask again. A failed send keeps the buttons and says so. Without
 * scripts the form posts and the page comes back thanked.
 */
export function initAscDocsFeedback({doc = document, storage = window.localStorage, send = POST} = {}) {
  const box = doc.querySelector('[data-asc-docs-feedback]');
  const form = box?.querySelector('form');
  if (!form) return;
  const key = prefix + box.getAttribute('data-asc-docs-feedback');
  const status = box.querySelector('[role="status"]');
  const buttons = Array.from(form.querySelectorAll('button[name="helpful"]'));

  const done = (value) => {
    for (const b of buttons) {
      b.disabled = true;
      b.setAttribute('aria-pressed', String(b.value === value));
    }
    if (status) status.textContent = status.getAttribute('data-thanks') ?? '';
  };

  const saved = safe(() => storage.getItem(key));
  if (saved === 'yes' || saved === 'no') done(saved);

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    const value = e.submitter?.value;
    if (value !== 'yes' && value !== 'no') return;
    for (const b of buttons) b.disabled = true;
    const data = new FormData(form);
    data.set('helpful', value);
    try {
      const resp = await send(form.getAttribute('action'), {data});
      if (!resp.ok) throw new Error(`feedback was not taken (${resp.status})`);
      safe(() => storage.setItem(key, value));
      done(value);
    } catch {
      for (const b of buttons) b.disabled = false;
      if (status) status.textContent = status.getAttribute('data-failed') ?? '';
    }
  });
}
