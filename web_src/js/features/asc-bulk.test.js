import {initAscBulk, syncBulk} from './asc-bulk.js';

function page() {
  document.body.innerHTML = `
    <form id="adm-bulk" data-bulk><span data-bulk-count data-one="1 selected" data-many="%d selected">Tick people</span>
      <button data-bulk-action>Suspend</button></form>
    <input type="checkbox" data-bulk-all>
    <input type="checkbox" data-bulk-row form="adm-bulk" name="ids" value="2">
    <input type="checkbox" data-bulk-row form="adm-bulk" name="ids" value="3">`;
  return document.querySelector('form');
}

test('the bar waits for a tick and counts them', () => {
  const form = page();
  initAscBulk();
  const button = form.querySelector('button');
  expect(button.disabled).toBe(true);
  const [a, b] = document.querySelectorAll('[data-bulk-row]');
  a.checked = true;
  a.dispatchEvent(new Event('change', {bubbles: true}));
  expect(form.querySelector('[data-bulk-count]').textContent).toBe('1 selected');
  expect(button.disabled).toBe(false);
  expect(document.querySelector('[data-bulk-all]').indeterminate).toBe(true);
  b.checked = true;
  expect(syncBulk(form)).toBe(2);
  expect(form.querySelector('[data-bulk-count]').textContent).toBe('2 selected');
});

test('the head tick ticks every row and back', () => {
  const form = page();
  initAscBulk();
  const all = document.querySelector('[data-bulk-all]');
  all.checked = true;
  all.dispatchEvent(new Event('change', {bubbles: true}));
  expect([...document.querySelectorAll('[data-bulk-row]')].every((r) => r.checked)).toBe(true);
  all.checked = false;
  all.dispatchEvent(new Event('change', {bubbles: true}));
  expect(syncBulk(form)).toBe(0);
  expect(form.querySelector('[data-bulk-count]').textContent).toBe('Tick people');
});
