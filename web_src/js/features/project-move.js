import {POST} from '../modules/fetch.js';

/** Puts the values into a message of the board (data-moved and the others): {card}, {column} and {id}. */
export function fillMessage(template, values) {
  return (template ?? '').replace(/\{(card|column|id)\}/g, (all, key) => values[key] ?? all);
}

const cardTitle = (card) => card.querySelector('.issue-card-title')?.textContent.trim() ?? '';
const columnTitle = (column) => column.querySelector('.project-column-title-label')?.textContent.trim() ?? '';

export function updateColumnCount(cards) {
  const count = cards.closest('.project-column')?.querySelector('.project-column-issue-count');
  if (count) count.textContent = String(cards.querySelectorAll(':scope > .issue-card').length);
}

/** Saves the order of the cards of one column. The result says whether it saved and, when it did not, the request id. */
export async function saveColumnOrder(cards) {
  const issues = Array.from(cards.querySelectorAll(':scope > .issue-card'), (card, i) => ({
    issueID: parseInt(card.getAttribute('data-issue')),
    sorting: i,
  }));
  try {
    const res = await POST(`${cards.getAttribute('data-url')}/move`, {data: {issues}});
    return {ok: res.ok, requestId: res.headers.get('X-Request-Id') ?? ''};
  } catch {
    return {ok: false, requestId: ''};
  }
}

function announce(board, text) {
  const status = board.querySelector('#project-board-status');
  if (status) status.textContent = text;
}

export function hideMoveError(board) {
  const error = board.querySelector('#project-board-error');
  if (error) error.hidden = true;
}

/** Shows a problem of the board in its error banner. */
export function showBoardError(board, text) {
  const error = board.querySelector('#project-board-error');
  if (!error) return;
  error.querySelector('.ds-banner-body').textContent = text;
  error.hidden = false;
}

/** Shows that the card did not move, with the request id when the hub answered. */
export function showMoveError(board, card, requestId) {
  const template = requestId ? board.getAttribute('data-move-failed') : board.getAttribute('data-move-failed-no-id');
  showBoardError(board, fillMessage(template, {card: cardTitle(card), id: requestId}));
}

/**
 * Tells the board how a move that is already in the page went: it announces the new column, or puts the card back
 * where it was (before `next` in `from`) and shows the error. The drag and drop of repo-projects.js uses it too.
 */
export async function settleMove(board, card, from, next, save) {
  const to = card.parentElement;
  board.setAttribute('aria-busy', 'true');
  const {ok, requestId} = await save(to);
  board.removeAttribute('aria-busy');
  if (ok) {
    hideMoveError(board);
    announce(board, fillMessage(board.getAttribute('data-moved'), {card: cardTitle(card), column: columnTitle(to.closest('.project-column'))}));
    return true;
  }
  from.insertBefore(card, next && next.parentElement === from ? next : null);
  updateColumnCount(from);
  updateColumnCount(to);
  announce(board, '');
  showMoveError(board, card, requestId);
  return false;
}

/** Moves a card to the end of another column and saves it. */
export async function moveCardTo(board, card, column, save = saveColumnOrder) {
  const from = card.parentElement;
  const to = column.querySelector('.cards');
  if (!to || to === from) return false;
  const next = card.nextElementSibling;
  to.append(card);
  updateColumnCount(from);
  updateColumnCount(to);
  announce(board, fillMessage(board.getAttribute('data-moving'), {card: cardTitle(card), column: columnTitle(column)}));
  return settleMove(board, card, from, next, save);
}

function closeMenu(menu, returnFocus) {
  const button = menu.previousElementSibling;
  menu.remove();
  button?.setAttribute('aria-expanded', 'false');
  if (returnFocus) button?.focus();
}

/** The menu of a card's Move button: one item for every other column, with the arrow keys, Home, End and Escape. */
export function openMoveMenu(board, button) {
  for (const open of board.querySelectorAll('.wk-pv-menu')) closeMenu(open, false);
  const card = button.closest('.issue-card');
  const here = card.closest('.project-column');
  const menu = document.createElement('div');
  menu.className = 'wk-pv-menu';
  menu.setAttribute('role', 'menu');
  menu.setAttribute('aria-label', button.getAttribute('aria-label') ?? '');
  for (const column of board.querySelectorAll('.project-column')) {
    if (column === here) continue;
    const item = document.createElement('button');
    item.type = 'button';
    item.className = 'wk-pv-menu-item';
    item.setAttribute('role', 'menuitem');
    item.setAttribute('tabindex', '-1');
    item.textContent = columnTitle(column);
    item.addEventListener('click', async () => {
      closeMenu(menu, false);
      await moveCardTo(board, card, column);
      card.querySelector('.project-card-move')?.focus();
    });
    menu.append(item);
  }
  const items = () => Array.from(menu.querySelectorAll('[role="menuitem"]'));
  menu.addEventListener('keydown', (e) => {
    const all = items();
    const at = all.indexOf(document.activeElement);
    let to = -1;
    if (e.key === 'ArrowDown') to = (at + 1) % all.length;
    else if (e.key === 'ArrowUp') to = (at - 1 + all.length) % all.length;
    else if (e.key === 'Home') to = 0;
    else if (e.key === 'End') to = all.length - 1;
    else if (e.key === 'Escape') {
      e.preventDefault();
      closeMenu(menu, true);
      return;
    } else if (e.key === 'Tab') {
      closeMenu(menu, false);
      return;
    }
    if (to < 0 || !all.length) return;
    e.preventDefault();
    all[to].focus();
  });
  menu.addEventListener('focusout', (e) => {
    if (menu.isConnected && !menu.contains(e.relatedTarget) && e.relatedTarget !== button) closeMenu(menu, false);
  });
  button.after(menu);
  button.setAttribute('aria-expanded', 'true');
  items()[0]?.focus();
  return menu;
}

/** The project board (templates/projects/view.tmpl): the Move button of every card, and the error banner. */
export function initProjectMove(doc = document) {
  const board = doc.querySelector('#project-board[data-moved]');
  if (!board) return;
  board.addEventListener('click', (e) => {
    const button = e.target.closest('.project-card-move');
    if (button) {
      e.preventDefault();
      const open = button.nextElementSibling?.matches('.wk-pv-menu');
      if (open) closeMenu(button.nextElementSibling, true);
      else openMoveMenu(board, button);
      return;
    }
    if (e.target.closest('#project-board-error .ds-banner-close')) hideMoveError(board);
  });
}
