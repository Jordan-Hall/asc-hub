import {initProjectBoard, refusalText, setColumnHidden, setColumnLanded, submitAddCard} from './project-board.js';

const page = `
<form class="project-add-card-form" data-url="/acme/nova/projects/1/cards">
  <div class="ds-field"><input name="ref" value="#12">
    <p id="project-add-card-err" data-failed="Not added. Request id {id}." data-failed-no-id="Not added. Check your connection." hidden></p>
  </div>
  <button type="submit">Add</button>
</form>
<div id="project-board" data-moved="Moved" data-column-failed="Column unchanged. Request id {id}." data-column-failed-no-id="Column unchanged.">
  <div id="project-board-error" hidden><p class="ds-banner-body"></p></div>
  <a class="project-column-visibility" data-url="/acme/nova/projects/1/3/hidden" data-hidden="true">Hide column</a>
  <a class="project-column-landed" data-url="/acme/nova/projects/1/3/landed" data-landed="true">Use as Landed column</a>
</div>`;

const answer = (status, body, id) => new Response(body, {status, headers: id ? {'X-Request-Id': id} : {}});
let fetch;
beforeEach(() => {
  document.body.innerHTML = page;
  fetch = vi.fn();
  vi.stubGlobal('fetch', fetch);
});
afterEach(() => vi.unstubAllGlobals());

test('a refusal says what the hub said, or gives the request id', () => {
  expect(refusalText({message: 'Already on the board.', requestId: 'r1'}, 'id {id}', 'none')).toBe('Already on the board.');
  expect(refusalText({message: '', requestId: 'r1'}, 'id {id}', 'none')).toBe('id r1');
  expect(refusalText({message: '', requestId: ''}, 'id {id}', 'none')).toBe('none');
});

test('Add card posts the reference and loads the board again', async () => {
  fetch.mockResolvedValue(answer(200, '{"ok":true}'));
  const reload = vi.fn();
  expect(await submitAddCard(document.querySelector('form'), reload)).toBe(true);
  const [url, init] = fetch.mock.calls[0];
  expect(url).toBe('/acme/nova/projects/1/cards');
  expect(init.body.toString()).toBe('ref=%2312');
  expect(reload).toHaveBeenCalled();
});

test('a refused card keeps the dialog open with the reason', async () => {
  fetch.mockResolvedValue(answer(422, '{"message":"That card is already on this board."}', 'r2'));
  const reload = vi.fn();
  const form = document.querySelector('form');
  expect(await submitAddCard(form, reload)).toBe(false);
  expect(reload).not.toHaveBeenCalled();
  const error = document.querySelector('#project-add-card-err');
  expect(error.hidden).toBe(false);
  expect(error.textContent).toBe('That card is already on this board.');
  expect(form.querySelector('.ds-field').classList.contains('is-err')).toBe(true);
  expect(document.activeElement).toBe(form.elements.ref);

  fetch.mockResolvedValue(answer(500, 'oops', 'req-55'));
  await submitAddCard(form, reload);
  expect(error.textContent).toBe('Not added. Request id req-55.');
});

test('hiding a column posts its new state; a failure shows the banner with the request id', async () => {
  const board = document.querySelector('#project-board');
  const control = document.querySelector('.project-column-visibility');
  fetch.mockResolvedValue(answer(200, '{}'));
  const reload = vi.fn();
  expect(await setColumnHidden(board, control, reload)).toBe(true);
  expect(fetch.mock.calls[0][0]).toBe('/acme/nova/projects/1/3/hidden');
  expect(fetch.mock.calls[0][1].body.toString()).toBe('hidden=true');
  expect(reload).toHaveBeenCalled();

  fetch.mockResolvedValue(answer(500, '', 'req-9'));
  expect(await setColumnHidden(board, control, reload)).toBe(false);
  expect(board.querySelector('#project-board-error').hidden).toBe(false);
  expect(board.querySelector('.ds-banner-body').textContent).toBe('Column unchanged. Request id req-9.');

  fetch.mockRejectedValue(new TypeError('offline'));
  await setColumnHidden(board, control, reload);
  expect(board.querySelector('.ds-banner-body').textContent).toBe('Column unchanged.');
});

test('the column menu item works from the keyboard', () => {
  fetch.mockResolvedValue(answer(500, ''));
  initProjectBoard();
  const control = document.querySelector('.project-column-visibility');
  control.dispatchEvent(new KeyboardEvent('keydown', {key: 'Tab', bubbles: true}));
  expect(fetch).not.toHaveBeenCalled();
  control.dispatchEvent(new KeyboardEvent('keydown', {key: 'Enter', bubbles: true}));
  expect(fetch).toHaveBeenCalledTimes(1);
});

test('Use as Landed column posts the new state; a failure shows the banner', async () => {
  const board = document.querySelector('#project-board');
  const control = document.querySelector('.project-column-landed');
  fetch.mockResolvedValue(answer(200, '{}'));
  const reload = vi.fn();
  expect(await setColumnLanded(board, control, reload)).toBe(true);
  expect(fetch.mock.calls[0][0]).toBe('/acme/nova/projects/1/3/landed');
  expect(fetch.mock.calls[0][1].body.toString()).toBe('landed=true');
  expect(reload).toHaveBeenCalled();

  fetch.mockResolvedValue(answer(500, '', 'req-10'));
  expect(await setColumnLanded(board, control, reload)).toBe(false);
  expect(board.querySelector('.ds-banner-body').textContent).toBe('Column unchanged. Request id req-10.');
});

test('the Landed menu item posts to its own address', () => {
  fetch.mockResolvedValue(answer(500, ''));
  const doc = document.implementation.createHTMLDocument('board');
  doc.body.innerHTML = page;
  initProjectBoard(doc);
  doc.querySelector('.project-column-landed').dispatchEvent(new MouseEvent('click', {bubbles: true}));
  expect(fetch).toHaveBeenCalledTimes(1);
  expect(fetch.mock.calls[0][0]).toBe('/acme/nova/projects/1/3/landed');
});
