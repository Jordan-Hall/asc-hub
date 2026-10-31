import {onSubmitConfirm, expireUndos} from './asc-confirm.js';

test('a form with data-submit-confirm is stopped unless the person agrees', () => {
  document.body.innerHTML = '<form id="f" data-submit-confirm="Block maya?"><button></button></form><form id="g"></form>';
  const make = (id) => {
    const e = new Event('submit', {cancelable: true});
    Object.defineProperty(e, 'target', {value: document.getElementById(id)});
    return e;
  };
  let asked = '';
  const no = make('f');
  expect(onSubmitConfirm(no, (t) => { asked = t; return false })).toBe(true);
  expect(asked).toBe('Block maya?');
  expect(no.defaultPrevented).toBe(true);
  const yes = make('f');
  expect(onSubmitConfirm(yes, () => true)).toBe(false);
  expect(yes.defaultPrevented).toBe(false);
  const other = make('g');
  expect(onSubmitConfirm(other, () => false)).toBe(false);
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
