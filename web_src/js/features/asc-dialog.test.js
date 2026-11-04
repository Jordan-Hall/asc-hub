import {reopenDialogs, onCloseClick} from './asc-dialog.js';

function click(el) {
  const e = new Event('click', {cancelable: true});
  Object.defineProperty(e, 'target', {value: el});
  return e;
}

test('the dialogs of refused forms open on load, once', () => {
  document.body.innerHTML = '<dialog id="a" data-asc-open></dialog><dialog id="b"></dialog><dialog id="c" data-asc-open open></dialog>';
  const opened = [];
  expect(reopenDialogs(document, (d) => opened.push(d.id))).toBe(1);
  expect(opened).toEqual(['a']);
});

test('a header close button closes its own dialog and nothing else', () => {
  document.body.innerHTML = '<dialog id="d" open><header><button data-asc-close><svg></svg></button></header></dialog><button id="other"></button>';
  const d = document.getElementById('d');
  let closed = 0;
  d.close = () => { closed++ };
  const e = click(d.querySelector('svg'));
  expect(onCloseClick(e)).toBe(true);
  expect(e.defaultPrevented).toBe(true);
  expect(closed).toBe(1);
  expect(onCloseClick(click(document.getElementById('other')))).toBe(false);
  expect(closed).toBe(1);
});
