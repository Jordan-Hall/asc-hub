// Keeps what a person is typing in a comment or a new issue on this device, so a closed tab or a lost connection does not
// lose it. The text of a textarea named "content" inside a markdown editor is stored a second after the last key, put back
// when the page opens with an empty field, and dropped when the form is sent or the field is emptied. Fields that open with
// text (an edit of a comment) are left alone. A draft lives 30 days and is at most 20 KB.

const prefix = 'asc-draft:';
const maxAge = 30 * 24 * 3600 * 1000;
const maxLength = 20 * 1024;
const delay = 1000;

export function draftKey(pathname, name) {
  return `${prefix}${pathname}:${name}`;
}

export function packDraft(text, now = Date.now()) {
  return JSON.stringify({t: text.slice(0, maxLength), at: now});
}

export function unpackDraft(raw, now = Date.now()) {
  if (!raw) return '';
  try {
    const {t, at} = JSON.parse(raw);
    if (typeof t !== 'string' || typeof at !== 'number' || now - at > maxAge) return '';
    return t;
  } catch {
    return '';
  }
}

function safe(fn) {
  try {
    return fn();
  } catch {
    // storage is blocked or full: the draft is simply not kept
    return undefined;
  }
}

export function initAscDrafts(root = document, storage = window.localStorage, pathname = window.location.pathname) {
  for (const area of root.querySelectorAll('.combo-markdown-editor textarea[name="content"]')) {
    const form = area.closest('form');
    if (!form || area.dataset.ascDraft) continue;
    area.dataset.ascDraft = '1';
    const key = draftKey(pathname, area.name);
    if (area.value === '') {
      const saved = unpackDraft(safe(() => storage.getItem(key)));
      if (saved) {
        area.value = saved;
        area.dispatchEvent(new Event('input', {bubbles: true}));
      }
    } else if (!area.defaultValue) {
      continue;
    }
    let timer = 0;
    area.addEventListener('input', () => {
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (area.value.trim() === '') safe(() => storage.removeItem(key));
        else safe(() => storage.setItem(key, packDraft(area.value)));
      }, delay);
    });
    form.addEventListener('submit', () => {
      clearTimeout(timer);
      safe(() => storage.removeItem(key));
    });
  }
}
