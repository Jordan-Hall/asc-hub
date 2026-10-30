import {decisionOf, applyDecision, filterRows, reuseTask, applyEstimate, applyQueue} from './asc-board.js';

function card() {
  document.body.innerHTML = `<section data-asc-needs><span data-asc-needs-count>2</span>
    <div data-asc-need><form data-asc-decide><button name="decision" value="allow">a</button><button name="decision" value="deny">d</button></form><p data-asc-need-status></p></div></section>
    <ul id="l"><li data-asc-row>Add retries</li><li data-asc-row>Update tables</li></ul>`;
  return document.querySelector('[data-asc-need]');
}

test('decisionOf accepts only the four words', () => {
  card();
  const form = document.querySelector('form');
  expect(decisionOf(form, form.querySelector('[value=allow]'))).toBe('allow');
  expect(decisionOf(form, {value: 'delete-everything'})).toBe('');
});

test('applyDecision greys the card and lowers the count on success only', () => {
  const c = card();
  const panel = document.querySelector('[data-asc-needs]');
  expect(applyDecision(c, panel, {ok: false, message: 'Not allowed'})).toBe(false);
  expect(c.querySelector('[data-asc-need-status]').textContent).toBe('Not allowed');
  expect(panel.querySelector('[data-asc-needs-count]').textContent).toBe('2');
  expect(applyDecision(c, panel, {ok: true, message: 'Allowed once.'})).toBe(true);
  expect(panel.querySelector('[data-asc-needs-count]').textContent).toBe('1');
  expect(c.classList.contains('is-done')).toBe(true);
  expect(c.querySelector('button').hasAttribute('disabled')).toBe(true);
});

test('server text is never parsed as markup', () => {
  const c = card();
  applyDecision(c, null, {ok: true, message: '<img src=x onerror=alert(1)>'});
  expect(c.querySelector('img')).toBeNull();
});

test('filterRows hides rows without the text', () => {
  card();
  const list = document.querySelector('#l');
  expect(filterRows(list, 'retries')).toBe(1);
  expect(list.children[1].hidden).toBe(true);
  expect(filterRows(list, '')).toBe(2);
});

test('reuse puts the text into the textarea and the agent into the field', () => {
  document.body.innerHTML = '<textarea data-asc-estimate-task></textarea><input data-asc-estimate-agent><button data-asc-reuse="Fix <b>it</b>" data-asc-reuse-agent="coder-2">r</button>';
  expect(reuseTask(document.querySelector('button'))).toBe(true);
  expect(document.querySelector('textarea').value).toBe('Fix <b>it</b>');
  expect(document.querySelector('input').value).toBe('coder-2');
  expect(document.querySelector('b')).toBeNull();
});

test('an estimate shows or hides its two lines and never parses markup', () => {
  document.body.innerHTML = `<div id="c"><p data-est-known hidden><span data-est-duration></span><span data-est-cost></span><span data-est-samples></span></p><p data-est-unknown>none</p></div>`;
  const c = document.querySelector('#c');
  applyEstimate(c, {known: true, duration: '10 min', cost: '<i>$1.60</i>', samples: 14});
  expect(c.querySelector('[data-est-known]').hidden).toBe(false);
  expect(c.querySelector('[data-est-unknown]').hidden).toBe(true);
  expect(c.querySelector('[data-est-cost]').textContent).toBe('<i>$1.60</i>');
  expect(c.querySelector('i')).toBeNull();
  applyEstimate(c, {known: false});
  expect(c.querySelector('[data-est-unknown]').hidden).toBe(false);
});

test('a row with a key matches by the start of the key only', () => {
  document.body.innerHTML = '<ul id="l"><li data-asc-row data-asc-key="src/a.rs">mentions config/</li><li data-asc-row data-asc-key="config/x">x</li></ul>';
  const list = document.querySelector('#l');
  expect(filterRows(list, 'src/')).toBe(1);
  expect(list.children[1].hidden).toBe(true);
  expect(filterRows(list, 'config/')).toBe(1);
  expect(list.children[0].hidden).toBe(true);
});

test('an empty queue reloads when an entry appears, and a full one when an entry is gone', () => {
  const empty = document.createElement('section');
  expect(applyQueue(empty, {entries: []})).toBe(false);
  expect(applyQueue(empty, {entries: [{change: 'c1', position: 1}]})).toBe(true);

  const full = document.createElement('section');
  full.innerHTML = '<ol><li data-q-change="c1"><span data-q-eta></span></li></ol>';
  expect(applyQueue(full, {entries: [{change: 'c1', position: 1, detail: 'checking'}]})).toBe(false);
  expect(full.querySelector('[data-q-eta]').textContent).toBe('checking');
  expect(applyQueue(full, {entries: []})).toBe(true);
});
