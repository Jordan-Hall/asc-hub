import {onSubmitConfirm, expireUndos} from './asc-confirm.js';

function submitEvent(id) {
  const e = new Event('submit', {cancelable: true});
  Object.defineProperty(e, 'target', {value: document.getElementById(id)});
  return e;
}

test('a form with data-confirm-modal is stopped, opens its modal and is sent only on approval', () => {
  document.body.innerHTML = '<form id="f" data-confirm-modal="#m"><input data-confirm-name value="maya"><button></button></form><form id="g"></form>';
  const f = document.getElementById('f');
  f.requestSubmit = () => { f.dispatchEvent(submitEvent('f')) };
  let approve = null;
  let opened = null;
  const open = (form, ok) => { opened = form; approve = ok };

  const first = submitEvent('f');
  expect(onSubmitConfirm(first, open)).toBe(true);
  expect(first.defaultPrevented).toBe(true);
  expect(opened.id).toBe('f');

  // approving sends the form again; that send is let through once
  let sent = 0;
  f.requestSubmit = () => {
    const again = submitEvent('f');
    if (!onSubmitConfirm(again, open)) sent++;
  };
  approve();
  expect(sent).toBe(1);
  expect(f.hasAttribute('data-confirmed')).toBe(false);

  const other = submitEvent('g');
  expect(onSubmitConfirm(other, open)).toBe(false);
  expect(other.defaultPrevented).toBe(false);
});

test('an undo offer is removed when its time is over, and a bad time keeps it', () => {
  document.body.innerHTML = '<div id="a" data-undo-ms="10000"></div><div id="b" data-undo-ms="abc"></div><div id="c" data-undo-ms="99999999"></div>';
  const jobs = [];
  expect(expireUndos(document, (fn, ms) => jobs.push([fn, ms]))).toBe(1);
  expect(jobs[0][1]).toBe(10000);
  jobs[0][0]();
  expect(document.getElementById('a')).toBeNull();
  expect(document.getElementById('b')).not.toBeNull();
  expect(document.getElementById('c')).not.toBeNull();
});
