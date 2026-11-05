import {POST} from '../modules/fetch.js';
import {showErrorToast} from '../modules/toast.js';

// Saves the colour mode and the density a signed-in person chooses (header menu, Settings, Appearance, the density
// switch of a list) with their account, so every browser opens the same way. The head script (base/head_style.tmpl)
// applies the choice at once and keeps it in this browser; <meta name="asc-prefs"> is there only for a signed-in
// person and names where to save it.

const themes = new Set(['system', 'light', 'dark']);
const densities = new Set(['comfy', 'compact']);

/** The preference a click chose, as the form to post, or null when the click chose none. */
export function choiceOf(target) {
  if (!(target instanceof Element)) return null;
  const theme = target.closest('[data-ds-theme-set]')?.getAttribute('data-ds-theme-set');
  if (themes.has(theme)) return {theme};
  const density = target.closest('[data-ds-density-set]')?.getAttribute('data-ds-density-set') ??
    target.closest('[data-asc-density] button[data-density]')?.getAttribute('data-density');
  if (densities.has(density)) return {density};
  return null;
}

/** Posts one choice; says so when the account did not take it. */
export async function save(meta, choice, send = POST) {
  try {
    const resp = await send(meta.getAttribute('content'), {data: new URLSearchParams(choice)});
    if (!resp.ok) throw new Error(`saving the preference answered ${resp.status}`);
    return true;
  } catch {
    showErrorToast(meta.getAttribute('data-failed'));
    return false;
  }
}

export function initAscPrefs({doc = document, send = POST} = {}) {
  const meta = doc.querySelector('meta[name="asc-prefs"]');
  if (!meta) return;
  doc.addEventListener('click', (e) => {
    const choice = choiceOf(e.target);
    if (choice) save(meta, choice, send);
  });
}
