// Blame on a phone (board Code): the avatar column opens the change behind a line in a bottom sheet (the desktop
// shows the same card on hover), and a long press on a line number selects that line, or extends the selection to it,
// then offers Share, Copy permalink and Reference in a new issue. The selection itself is repo-code's: the long press
// sends it the click a tap would, so the hash, the highlight and the permalink stay in one place.

import {clippie} from 'clippie';
import {toAbsoluteUrl} from '../utils.js';

const NARROW = '(max-width: 1023.98px)';
export const HOLD_MS = 500;
const SLOP_PX = 10;

// The sheet's heading for lines a..b, from the localized patterns on the dialog.
export function lineTitle(dialog, a, b) {
  if (a === b) return dialog.getAttribute('data-t-line').replace('{a}', String(a));
  return dialog.getAttribute('data-t-lines').replace('{a}', String(a)).replace('{b}', String(b));
}

// The selected lines as [first, last], from the location hash repo-code wrote.
export function selectedLines(hash) {
  const m = /^#L(\d+)(?:-L(\d+))?$/.exec(hash);
  if (!m) return null;
  const a = Number(m[1]);
  return [a, m[2] ? Number(m[2]) : a];
}

function show(dialog, part) {
  for (const el of dialog.querySelectorAll('[data-blame-sheet-change], [data-blame-sheet-line]')) {
    el.hidden = !el.matches(part);
  }
  if (!dialog.open) dialog.showModal();
}

// Fills the sheet with the change of the row whose avatar was tapped.
export function openChange(dialog, button) {
  const card = button.closest('.blame-info')?.querySelector('.rp-blame-card');
  if (!card) return false;
  dialog.querySelector('[data-blame-sheet-h]').textContent = card.querySelector('.rp-blame-card-t')?.textContent ?? '';
  const body = dialog.querySelector('[data-blame-sheet-change]');
  body.replaceChildren(...Array.from(card.children).filter((el) => !el.matches('.rp-blame-card-t')).map((el) => el.cloneNode(true)));
  show(dialog, '[data-blame-sheet-change]');
  return true;
}

// Fills the sheet with the selected lines and their links.
export function openLines(dialog, root = document) {
  const lines = selectedLines(window.location.hash);
  if (!lines) return false;
  dialog.querySelector('[data-blame-sheet-h]').textContent = lineTitle(dialog, lines[0], lines[1]);
  const issue = dialog.querySelector('[data-blame-issue]');
  const ref = root.querySelector('.code-line-menu a.ref-in-new-issue');
  if (issue && ref?.getAttribute('href')) issue.setAttribute('href', ref.getAttribute('href'));
  dialog.querySelector('[data-blame-share]').hidden = typeof navigator.share !== 'function';
  show(dialog, '[data-blame-sheet-line]');
  return true;
}

function permalink(root = document) {
  const a = root.querySelector('.code-line-menu a.copy-line-permalink');
  return a ? toAbsoluteUrl(a.getAttribute('data-url')) : window.location.href;
}

export function initAscBlameSheet() {
  const dialog = document.querySelector('dialog[data-blame-sheet]');
  if (!dialog || typeof dialog.showModal !== 'function') return;
  const narrow = window.matchMedia(NARROW);

  document.addEventListener('click', (e) => {
    const button = e.target.closest?.('[data-blame-open]');
    if (button && narrow.matches && openChange(dialog, button)) e.preventDefault();
  });

  dialog.querySelector('[data-blame-copy]').addEventListener('click', async () => {
    await clippie(permalink());
    dialog.close();
  });
  dialog.querySelector('[data-blame-share]').addEventListener('click', async () => {
    try {
      await navigator.share({title: dialog.querySelector('[data-blame-sheet-h]').textContent, url: permalink()});
      dialog.close();
    } catch {
      // the share sheet was dismissed: keep ours open for Copy permalink
    }
  });

  let timer = 0;
  let start = null;
  let firedAt = 0;
  const cancel = () => {
    window.clearTimeout(timer);
    timer = 0;
  };
  document.addEventListener('pointerdown', (e) => {
    if (e.pointerType === 'mouse' || !e.target.matches?.('.lines-num span[id^="L"]')) return;
    const span = e.target;
    start = {x: e.clientX, y: e.clientY};
    cancel();
    timer = window.setTimeout(() => {
      timer = 0;
      firedAt = Date.now();
      const active = document.querySelector('.code-view tr.active td.lines-code');
      const extend = Boolean(active) && active.getAttribute('rel') !== span.id;
      span.dispatchEvent(new MouseEvent('click', {bubbles: true, shiftKey: extend}));
      openLines(dialog);
    }, HOLD_MS);
  });
  document.addEventListener('pointermove', (e) => {
    if (timer && start && Math.hypot(e.clientX - start.x, e.clientY - start.y) > SLOP_PX) cancel();
  });
  for (const type of ['pointerup', 'pointercancel']) document.addEventListener(type, cancel);
  // The press already selected: the click and the menu the browser sends after it would undo or cover that.
  const swallow = (e) => {
    const pressing = timer !== 0 || Date.now() - firedAt < 800;
    if (pressing && e.isTrusted && e.target.closest?.('.lines-num')) {
      e.preventDefault();
      e.stopImmediatePropagation();
    }
  };
  document.addEventListener('click', swallow, true);
  document.addEventListener('contextmenu', swallow, true);
}
