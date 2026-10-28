// Row density switch (boards History and Refs): a group with data-asc-density="<selector of the list>" holds buttons
// with data-density="comfy" or "compact". The choice is kept in localStorage under asc-density and put on the list
// as data-density; storage that is blocked only costs the memory of the choice.

const storageKey = 'asc-density';
const allowed = new Set(['comfy', 'compact']);

export function readDensity(storage = window.localStorage) {
  try {
    const value = storage.getItem(storageKey);
    return allowed.has(value) ? value : 'comfy';
  } catch {
    return 'comfy';
  }
}

export function applyDensity(group, value, storage = window.localStorage) {
  if (!allowed.has(value)) return false;
  const target = document.querySelector(group.getAttribute('data-asc-density') || '');
  if (target) target.setAttribute('data-density', value);
  for (const btn of group.querySelectorAll('button[data-density]')) {
    btn.setAttribute('aria-pressed', String(btn.getAttribute('data-density') === value));
  }
  try {
    storage.setItem(storageKey, value);
  } catch {
    // blocked storage: the choice lasts for this page only
  }
  return true;
}

export function initAscDensity() {
  for (const group of document.querySelectorAll('[data-asc-density]')) {
    applyDensity(group, readDensity());
    group.addEventListener('click', (e) => {
      const btn = e.target instanceof Element ? e.target.closest('button[data-density]') : null;
      if (btn) applyDensity(group, btn.getAttribute('data-density'));
    });
  }
}
