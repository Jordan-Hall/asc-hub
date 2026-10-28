import {decisionOf, applyDecision, filterRows} from './asc-board.js';

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
