// The order of the repositories pinned to a profile (board Profile: "Pins reorder by drag or by the Customize
// dialog"). The rows of the Customize list (templates/user/settings/pins_form.tmpl) move by drag or by Move up and
// Move down, and each move is announced; the owner's cards on the profile move by drag and the new order is saved
// at once. On the profile, Customize opens the list in a dialog and saves it without leaving the page; without
// script it is the settings page.
import {POST} from '../modules/fetch.js';
import {showErrorToast} from '../modules/toast.js';

const pinUrl = '/user/settings/pins';

// fill puts the arguments in place of the %s marks of a translated text, in order.
export function fill(text, ...args) {
  let i = 0;
  return text.replace(/%s/g, () => String(args[i++] ?? ''));
}

// moveRow moves an item one place up (by < 0) or down; it reports whether it moved.
export function moveRow(row, by) {
  if (by < 0 && row.previousElementSibling) {
    row.previousElementSibling.before(row);
    return true;
  }
  if (by > 0 && row.nextElementSibling) {
    row.nextElementSibling.after(row);
    return true;
  }
  return false;
}

// placeOver puts the dragged item in the place of the one it is over: after it when it came from above, before it
// when it came from below, so the order follows the pointer in a list and in a grid of cards.
export function placeOver(dragged, over) {
  if (!over || over === dragged || over.parentElement !== dragged.parentElement) return false;
  const items = Array.from(dragged.parentElement.children);
  if (items.indexOf(dragged) < items.indexOf(over)) {
    over.after(dragged);
  } else {
    over.before(dragged);
  }
  return true;
}

// initDragSort lets the draggable children of list be reordered by drag; onDrop runs once an item is let go.
export function initDragSort(list, onDrop) {
  let dragged = null;
  list.addEventListener('dragstart', (e) => {
    dragged = e.target.closest?.('[draggable="true"]');
    if (!dragged || dragged.parentElement !== list) {
      dragged = null;
      return;
    }
    if (e.dataTransfer) {
      e.dataTransfer.effectAllowed = 'move';
      e.dataTransfer.setData('text/plain', dragged.dataset.id || '');
    }
    dragged.classList.add('is-dragging');
  });
  list.addEventListener('dragover', (e) => {
    if (!dragged) return;
    e.preventDefault();
    placeOver(dragged, e.target.closest?.('[draggable="true"]'));
  });
  list.addEventListener('drop', (e) => {
    if (!dragged) return;
    e.preventDefault();
    onDrop(dragged);
  });
  list.addEventListener('dragend', () => {
    dragged?.classList.remove('is-dragging');
    dragged = null;
  });
}

function rowName(row) {
  return row.querySelector('label')?.textContent.trim() ?? '';
}

function announce(list, row) {
  const status = list.closest('form')?.querySelector('[data-asc-pins-status]');
  if (!status) return;
  status.textContent = fill(list.dataset.moved || '', rowName(row), Array.from(list.children).indexOf(row) + 1);
}

function initRows(list) {
  list.addEventListener('click', (e) => {
    const button = e.target.closest?.('[data-asc-pin-up], [data-asc-pin-down]');
    if (!button) return;
    const row = button.closest('.wk-pin-row');
    if (row && moveRow(row, button.matches('[data-asc-pin-up]') ? -1 : 1)) {
      button.focus(); // moving the row takes the focus away from the button
      announce(list, row);
    }
  });
  initDragSort(list, (row) => announce(list, row));
}

// cardOrder is what the drag of the cards sends: the token of the dialog form, the cards in order, inline=1.
export function cardOrder(cards, token) {
  const data = new FormData();
  data.append('_csrf', token);
  for (const card of cards.children) data.append('repo', card.dataset.id);
  data.append('inline', '1');
  return data;
}

async function saveForm(form, send) {
  const data = new FormData(form);
  data.append('inline', '1');
  const error = form.querySelector('[data-asc-pins-error]');
  try {
    const res = await send(form.getAttribute('action') || pinUrl, {data});
    if (res.status === 204) return true;
    error.textContent = (await res.text()).trim() || form.dataset.failed;
  } catch {
    error.textContent = form.dataset.failed;
  }
  error.classList.remove('tw-hidden');
  return false;
}

export function initAscPins({doc = document, send = POST, reload = () => window.location.reload()} = {}) {
  for (const list of doc.querySelectorAll('[data-asc-pins]')) initRows(list);

  const dialog = doc.querySelector('[data-asc-pins-dialog]');
  const form = dialog?.querySelector('[data-asc-pins-inline]');
  if (dialog && form) {
    for (const open of doc.querySelectorAll('[data-asc-pins-open]')) {
      open.addEventListener('click', (e) => {
        e.preventDefault();
        dialog.showModal();
        dialog.querySelector('input[name="repo"]')?.focus();
      });
    }
    form.querySelector('[data-asc-pins-close]')?.addEventListener('click', () => dialog.close());
    form.addEventListener('submit', async (e) => {
      e.preventDefault();
      if (await saveForm(form, send)) reload();
    });
  }

  const cards = doc.querySelector('[data-asc-pins-cards]');
  const token = form?.querySelector('input[name="_csrf"]')?.value;
  if (!cards || !token) return;
  initDragSort(cards, async () => {
    try {
      const res = await send(form.getAttribute('action') || pinUrl, {data: cardOrder(cards, token)});
      if (res.status === 204) return;
    } catch {
      // shown below
    }
    showErrorToast(form.dataset.failed);
  });
}
