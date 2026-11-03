import {fillMessage, initProjectMove, moveCardTo, openMoveMenu, saveColumnOrder, settleMove} from './project-move.js';

const boardHtml = `
<div id="project-board" data-moved="Moved {card} to {column}." data-moving="Moving {card} to {column}…"
  data-move-failed="{card} could not be moved. Request id {id}." data-move-failed-no-id="{card} could not be moved.">
  <p id="project-board-status" role="status"></p>
  <div id="project-board-error" hidden><p class="ds-banner-body"></p><button class="ds-banner-close">x</button></div>
  <div class="board sortable">
    <div class="project-column" data-id="1"><div class="project-column-issue-count">2</div><span class="project-column-title-label">Backlog</span>
      <div class="ui cards" data-url="/acme/nova/projects/1/1">
        <div class="issue-card" data-issue="11"><a class="issue-card-title">Fix login</a><button class="project-card-move" aria-label="Move Fix login">Move</button></div>
        <div class="issue-card" data-issue="12"><a class="issue-card-title">Docs</a><button class="project-card-move" aria-label="Move Docs">Move</button></div>
      </div>
    </div>
    <div class="project-column" data-id="2"><div class="project-column-issue-count">0</div><span class="project-column-title-label">Doing</span>
      <div class="ui cards" data-url="/acme/nova/projects/1/2"></div>
    </div>
    <div class="project-column" data-id="3"><div class="project-column-issue-count">0</div><span class="project-column-title-label">Done</span>
      <div class="ui cards" data-url="/acme/nova/projects/1/3"></div>
    </div>
  </div>
</div>`;

const setup = () => {
  document.body.innerHTML = boardHtml;
  const board = document.querySelector('#project-board');
  const columns = board.querySelectorAll('.project-column');
  const card = board.querySelector('[data-issue="11"]');
  return {board, columns, card};
};
const counts = (board) => Array.from(board.querySelectorAll('.project-column-issue-count'), (c) => c.textContent);
const key = (el, k) => el.dispatchEvent(new KeyboardEvent('keydown', {key: k, bubbles: true}));

test('a message gets the card, the column and the request id', () => {
  expect(fillMessage('Moved {card} to {column}.', {card: 'Fix login', column: 'Done'})).toBe('Moved Fix login to Done.');
  expect(fillMessage('Request id {id}.', {})).toBe('Request id {id}.');
  expect(fillMessage(null, {})).toBe('');
});

test('a move saves the new column, updates the counts and announces it', async () => {
  const {board, columns, card} = setup();
  const save = vi.fn(async () => ({ok: true, requestId: ''}));
  expect(await moveCardTo(board, card, columns[2], save)).toBe(true);
  expect(save).toHaveBeenCalledWith(columns[2].querySelector('.cards'));
  expect(card.parentElement).toBe(columns[2].querySelector('.cards'));
  expect(counts(board)).toEqual(['1', '0', '1']);
  expect(board.querySelector('#project-board-status').textContent).toBe('Moved Fix login to Done.');
  expect(board.hasAttribute('aria-busy')).toBe(false);
  expect(board.querySelector('#project-board-error').hidden).toBe(true);
});

test('a move the hub refuses puts the card back where it was and shows the request id', async () => {
  const {board, columns, card} = setup();
  expect(await moveCardTo(board, card, columns[1], async () => ({ok: false, requestId: 'req-7f3a'}))).toBe(false);
  expect(columns[0].querySelector('.cards').firstElementChild).toBe(card);
  expect(counts(board)).toEqual(['2', '0', '0']);
  const error = board.querySelector('#project-board-error');
  expect(error.hidden).toBe(false);
  expect(error.textContent).toContain('Fix login could not be moved. Request id req-7f3a.');
  initProjectMove();
  board.querySelector('.ds-banner-close').click();
  expect(error.hidden).toBe(true);
});

test('a move without an answer says so without a request id', async () => {
  const {board, card} = setup();
  const from = card.parentElement;
  const next = card.nextElementSibling;
  document.querySelector('[data-id="3"] .cards').append(card);
  await settleMove(board, card, from, next, async () => ({ok: false, requestId: ''}));
  expect(from.firstElementChild).toBe(card);
  expect(board.querySelector('#project-board-error .ds-banner-body').textContent).toBe('Fix login could not be moved.');
});

test('the menu lists the other columns and works from the keyboard', async () => {
  const {board, card} = setup();
  const button = card.querySelector('.project-card-move');
  const menu = openMoveMenu(board, button);
  const items = Array.from(menu.querySelectorAll('[role="menuitem"]'), (i) => i.textContent);
  expect(items).toEqual(['Doing', 'Done']);
  expect(button.getAttribute('aria-expanded')).toBe('true');
  expect(document.activeElement.textContent).toBe('Doing');
  key(document.activeElement, 'ArrowDown');
  expect(document.activeElement.textContent).toBe('Done');
  key(document.activeElement, 'ArrowDown');
  expect(document.activeElement.textContent).toBe('Doing');
  key(document.activeElement, 'End');
  expect(document.activeElement.textContent).toBe('Done');
  key(document.activeElement, 'Escape');
  expect(menu.isConnected).toBe(false);
  expect(button.getAttribute('aria-expanded')).toBe('false');
  expect(document.activeElement).toBe(button);
});

test('the order of a column is posted, and the request id of a refusal is kept', async () => {
  const {columns} = setup();
  const fetch = vi.fn(async () => new Response('', {status: 500, headers: {'X-Request-Id': 'req-91c2'}}));
  vi.stubGlobal('fetch', fetch);
  try {
    expect(await saveColumnOrder(columns[0].querySelector('.cards'))).toEqual({ok: false, requestId: 'req-91c2'});
    const [url, init] = fetch.mock.calls[0];
    expect(url).toBe('/acme/nova/projects/1/1/move');
    expect(JSON.parse(init.body)).toEqual({issues: [{issueID: 11, sorting: 0}, {issueID: 12, sorting: 1}]});
    fetch.mockImplementation(async () => {
      throw new TypeError('offline');
    });
    expect(await saveColumnOrder(columns[0].querySelector('.cards'))).toEqual({ok: false, requestId: ''});
  } finally {
    vi.unstubAllGlobals();
  }
});
