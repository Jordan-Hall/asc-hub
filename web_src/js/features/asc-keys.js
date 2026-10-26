// Keyboard help of the signed-in app pages (dashboard, inbox): j and k move through a list of rows, s opens
// the snooze menu, p pins, "g" then "i" goes to the inbox, and a select that carries data-asc-autosubmit sends
// its form when it changes. Every key is ignored while a field has the focus or a modifier key is held, and
// every key has a button or link on the page that does the same, so nothing depends on the keyboard.

const chordMs = 1200;

export function isTypingTarget(el) {
  if (!el || !el.tagName) return false;
  const tag = el.tagName.toLowerCase();
  return tag === 'input' || tag === 'textarea' || tag === 'select' || el.isContentEditable === true;
}

// Returns the index of the row to focus after pressing j (1) or k (-1), given the index that has the focus
// now (-1 when none has) and the number of rows. It never wraps.
export function nextRowIndex(current, step, count) {
  if (count <= 0) return -1;
  if (current < 0) return step > 0 ? 0 : count - 1;
  return Math.min(count - 1, Math.max(0, current + step));
}

function rows() {
  return Array.from(document.querySelectorAll('[data-asc-list] [data-asc-row]'));
}

function moveRow(step) {
  const list = rows();
  if (!list.length) return;
  const at = list.indexOf(document.activeElement);
  const selected = list.findIndex((el) => el.getAttribute('aria-current') === 'true');
  const next = nextRowIndex(at >= 0 ? at : selected, step, list.length);
  if (next >= 0) list[next].focus();
}

let keyHandler = null;

export function initAscKeys() {
  for (const select of document.querySelectorAll('select[data-asc-autosubmit]')) {
    select.addEventListener('change', () => select.form?.requestSubmit());
  }

  let chord = 0; // time of the last "g"
  if (keyHandler) document.removeEventListener('keydown', keyHandler);
  keyHandler = (e) => {
    if (e.defaultPrevented || e.altKey) return;
    // Ctrl K and "/" go to the search box of the header
    const search = document.querySelector('input[data-asc-search]');
    const isCtrlK = (e.ctrlKey || e.metaKey) && !e.shiftKey && e.key.toLowerCase() === 'k';
    if (search && (isCtrlK || (e.key === '/' && !e.ctrlKey && !e.metaKey && !isTypingTarget(e.target)))) {
      e.preventDefault();
      search.focus();
      search.select();
      return;
    }
    if (e.ctrlKey || e.metaKey || e.shiftKey) return;
    if (isTypingTarget(e.target)) return;
    const now = Date.now();
    switch (e.key) {
      case 'j':
        moveRow(1);
        break;
      case 'k':
        moveRow(-1);
        break;
      case 's': {
        const details = document.querySelector('[data-asc-snooze]');
        if (!details) return;
        details.open = !details.open;
        if (details.open) details.querySelector('button')?.focus();
        break;
      }
      case 'p':
        document.querySelector('[data-asc-pin]')?.click();
        break;
      case 'g':
        chord = now;
        return;
      case 'i': {
        if (now - chord > chordMs) return;
        const link = document.querySelector('a[data-asc-inbox-link]');
        if (link) window.location.assign(link.href);
        break;
      }
      default:
        return;
    }
    chord = 0;
  };
  document.addEventListener('keydown', keyHandler);
}

const countKey = 'asc-inbox-count';
const countMaxAgeMs = 60 * 1000;

// Shows how many items need the person on the inbox link of the header. The answer is kept for a minute in
// the session storage, so moving between pages asks once; storage that is blocked only costs a request.
export async function initAscInboxBadge() {
  const link = document.querySelector('a[data-asc-count-url]');
  const badge = link?.querySelector('[data-asc-count]');
  if (!badge) return;
  const show = (n) => {
    if (!Number.isInteger(n) || n <= 0) {
      badge.classList.add('tw-hidden');
      return;
    }
    badge.textContent = String(n);
    badge.classList.remove('tw-hidden');
  };
  try {
    const cached = JSON.parse(sessionStorage.getItem(countKey) || 'null');
    if (cached && Date.now() - cached.at < countMaxAgeMs) {
      show(cached.n);
      return;
    }
  } catch {
    // no storage: ask the server
  }
  try {
    const res = await fetch(link.getAttribute('data-asc-count-url'), {credentials: 'same-origin', cache: 'no-store'});
    if (!res.ok) return;
    const data = await res.json();
    show(data.count);
    try {
      sessionStorage.setItem(countKey, JSON.stringify({at: Date.now(), n: data.count}));
    } catch {
      // storage is full or blocked
    }
  } catch {
    // the badge is a convenience: stay quiet when the request fails
  }
}
