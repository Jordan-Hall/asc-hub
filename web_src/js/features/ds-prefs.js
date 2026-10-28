// Preferences that belong to this browser (the same place the density lives): reduce motion and keyboard
// shortcuts. A switch with data-pref="<name>" applies at once, stores "1" or "0" under asc-pref-<name> and says
// so in a toast that carries Undo; there is no Save step. data-pref-default is the value when nothing is stored.
// The root element carries data-pref-<name>="on|off" so the style sheet can follow it; the head script of
// base/head_style.tmpl sets the same attribute before the first paint.

import {showDsToast} from './ds-toast.js';

export function readPref(name, fallback = '0') {
  try {
    const v = window.localStorage.getItem(`asc-pref-${name}`);
    return v === '1' || v === '0' ? v : fallback;
  } catch {
    return fallback;
  }
}

export function writePref(name, value) {
  try {
    window.localStorage.setItem(`asc-pref-${name}`, value);
  } catch {
    // storage is blocked: the choice holds for this page only
  }
  document.documentElement.setAttribute(`data-pref-${name}`, value === '1' ? 'on' : 'off');
}

// Single-key shortcuts (j, k, s, p, g i, ?) follow the switch; Ctrl K and "/" for the palette stay on.
export function shortcutsEnabled() {
  return readPref('keys', '1') === '1';
}

export function initPrefs() {
  for (const box of document.querySelectorAll('input[type="checkbox"][data-pref]')) {
    const name = box.getAttribute('data-pref');
    const set = (value, announce) => {
      writePref(name, value);
      box.checked = value === '1';
      if (!announce) return;
      const previous = value === '1' ? '0' : '1';
      showDsToast(box.getAttribute(value === '1' ? 'data-toast-on' : 'data-toast-off') || '', {
        actionLabel: box.getAttribute('data-toast-undo') || '',
        onAction: () => set(previous, false),
      });
    };
    box.checked = readPref(name, box.getAttribute('data-pref-default') || '0') === '1';
    box.addEventListener('change', () => set(box.checked ? '1' : '0', true));
  }
}
