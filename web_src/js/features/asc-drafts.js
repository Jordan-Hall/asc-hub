// Keeps what a person is typing in a comment or a new issue on this device, so a closed tab or a lost connection does not
// lose it. The text of a textarea named "content" inside a markdown editor is stored a second after the last key, put back
// when the page opens with an empty field, and dropped when the form is sent or the field is emptied. Fields that open with
// text (an edit of a comment) are left alone. A draft lives 30 days and is at most 20 KB.
//
// A form sent while the device is offline is held (board Errors, Offline): the draft stays, a note says the edit is saved on
// this device and will be sent when the connection returns, and the form is sent then; Retry now sends it at once.

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

/** Whether any draft is kept on this device (the offline page says so). */
export function hasDrafts(storage = window.localStorage) {
  return Boolean(safe(() => Array.from({length: storage.length}, (_, i) => storage.key(i)).some((k) => k?.startsWith(prefix))));
}

const held = new Map(); // form: how to tell the device is online
let listening = false;

function send(form, online) {
  if (!online() || !form.isConnected) return;
  held.delete(form);
  form.querySelector('[data-asc-draft-held]')?.remove();
  form.requestSubmit();
}

function hold(form, online) {
  held.set(form, online);
  if (!listening) {
    listening = true;
    window.addEventListener('online', () => {
      for (const [f, isOnline] of Array.from(held)) send(f, isOnline);
    });
  }
  if (form.querySelector('[data-asc-draft-held]')) return;
  const note = document.querySelector('template[data-asc-draft-offline]')?.content.firstElementChild?.cloneNode(true);
  if (!note) return;
  note.setAttribute('data-asc-draft-held', '');
  note.querySelector('button')?.addEventListener('click', () => send(form, online));
  form.prepend(note);
}

export function initAscDrafts(root = document, storage = window.localStorage, pathname = window.location.pathname, online = () => navigator.onLine !== false) {
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
    form.addEventListener('submit', (e) => {
      clearTimeout(timer);
      if (!online()) {
        // held: the scripts that send the form by fetch never see it
        e.preventDefault();
        e.stopImmediatePropagation();
        if (area.value.trim() !== '') safe(() => storage.setItem(key, packDraft(area.value)));
        hold(form, online);
        return;
      }
      safe(() => storage.removeItem(key));
    });
  }
}
