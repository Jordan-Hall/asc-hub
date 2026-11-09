import {POST} from '../modules/fetch.js';
import {fillMessage, showBoardError} from './project-move.js';

/** Posts a form of the board. A refusal keeps the hub's message when it sent one, and the request id. */
export async function postBoardForm(url, fields) {
  try {
    const res = await POST(url, {data: new URLSearchParams(fields)});
    if (res.ok) return {ok: true, message: '', requestId: ''};
    let message = '';
    try {
      message = (await res.json())?.message ?? '';
    } catch {}
    return {ok: false, message, requestId: res.headers.get('X-Request-Id') ?? ''};
  } catch {
    return {ok: false, message: '', requestId: ''};
  }
}

/** The words of a refusal: the hub's own message, or the template with the request id, or the one without. */
export function refusalText(result, withId, withoutId) {
  if (result.message) return result.message;
  return result.requestId ? fillMessage(withId, {id: result.requestId}) : withoutId ?? '';
}

/** The Add card dialog: the card lands in the default column, so the board loads again to show it. */
export async function submitAddCard(form, reload = () => window.location.reload()) {
  const error = form.querySelector('#project-add-card-err');
  const submit = form.querySelector('[type="submit"]');
  const field = form.querySelector('.ds-field');
  error.hidden = true;
  field?.classList.remove('is-err');
  form.elements.ref.removeAttribute('aria-invalid');
  submit.disabled = true;
  form.setAttribute('aria-busy', 'true');
  const result = await postBoardForm(form.getAttribute('data-url'), {ref: form.elements.ref.value});
  form.removeAttribute('aria-busy');
  submit.disabled = false;
  if (result.ok) {
    reload();
    return true;
  }
  error.textContent = refusalText(result, error.getAttribute('data-failed'), error.getAttribute('data-failed-no-id'));
  error.hidden = false;
  field?.classList.add('is-err');
  form.elements.ref.setAttribute('aria-invalid', 'true');
  form.elements.ref.focus();
  return false;
}

/** Hide column (the column menu) and Show (the hidden columns bar). */
export async function setColumnHidden(board, control, reload = () => window.location.reload()) {
  const result = await postBoardForm(control.getAttribute('data-url'), {hidden: control.getAttribute('data-hidden')});
  if (result.ok) {
    reload();
    return true;
  }
  showBoardError(board, refusalText(result, board.getAttribute('data-column-failed'), board.getAttribute('data-column-failed-no-id')));
  return false;
}

/** Use as Landed column and Stop using as Landed column (the column menu): a card moves there when its stack lands. */
export async function setColumnLanded(board, control, reload = () => window.location.reload()) {
  const result = await postBoardForm(control.getAttribute('data-url'), {landed: control.getAttribute('data-landed')});
  if (result.ok) {
    reload();
    return true;
  }
  showBoardError(board, refusalText(result, board.getAttribute('data-column-failed'), board.getAttribute('data-column-failed-no-id')));
  return false;
}

/** The project board (templates/projects/view.tmpl): Add card, hiding and showing columns, and the Landed column. */
export function initProjectBoard(doc = document) {
  const board = doc.querySelector('#project-board[data-moved]');
  if (!board) return;
  const form = doc.querySelector('.project-add-card-form');
  form?.addEventListener('submit', (e) => {
    e.preventDefault();
    submitAddCard(form);
  });
  const visibility = (e) => {
    const control = e.target.closest('.project-column-visibility, .project-column-landed');
    if (!control) return;
    if (e.type === 'keydown' && e.key !== 'Enter' && e.key !== ' ') return;
    e.preventDefault();
    if (control.classList.contains('project-column-landed')) {
      setColumnLanded(board, control);
    } else {
      setColumnHidden(board, control);
    }
  };
  doc.addEventListener('click', visibility);
  doc.addEventListener('keydown', visibility);
}
