import {addChips, entriesOf, initAscPolicyForm, step} from './asc-policy-form.js';

const form = `<form>
  <div data-policy-stepper>
    <button type="button" data-step="-1" hidden>-</button>
    <input type="number" name="approvals" min="0" max="3" value="2">
    <button type="button" data-step="1" hidden>+</button>
  </div>
  <fieldset data-policy-chips="checks">
    <ul data-chips-list><li><label class="ast-chip"><input type="checkbox" name="checks" value="build" checked><span>build</span></label></li></ul>
    <input name="checks_add" data-chips-input>
    <button type="button" data-chips-add hidden>Add</button>
  </fieldset>
</form>`;

test('entriesOf splits lines and commas and drops blanks', () => {
  expect(entriesOf(' a ,b\n\n c\r\n')).toEqual(['a', 'b', 'c']);
  expect(entriesOf('  ')).toEqual([]);
});

test('step stays within min and max and reads an empty field as zero', () => {
  document.body.innerHTML = '<input type="number" min="0" max="3" value="2">';
  const input = document.querySelector('input');
  let changes = 0;
  input.addEventListener('input', () => changes++);
  step(input, 1);
  expect(input.value).toBe('3');
  step(input, 1);
  expect(input.value).toBe('3');
  input.value = '';
  step(input, -1);
  expect(input.value).toBe('0');
  expect(changes).toBe(3);
});

test('addChips makes ticked chips, ticks an existing one again and empties the field', () => {
  document.body.innerHTML = form;
  const field = document.querySelector('[data-policy-chips]');
  const build = field.querySelector('input[value=build]');
  field.querySelector('[data-chips-input]').value = 'lint, build, <e2e>'; // a one-line field: its value holds no line breaks
  addChips(field);
  const boxes = Array.from(field.querySelectorAll('input[type=checkbox]'));
  // the checked attribute: happy-dom unticks same-name checkboxes when a new ticked one is added, like radio buttons;
  // browsers do not
  expect(boxes.map((b) => [b.name, b.value, b.defaultChecked])).toEqual([
    ['checks', 'build', true],
    ['checks', 'lint', true],
    ['checks', '<e2e>', true],
  ]);
  expect(boxes.at(-1).checked).toBe(true);
  expect(field.querySelector('li:last-child span').textContent).toBe('<e2e>');
  expect(field.querySelector('li:last-child span').innerHTML).toBe('&lt;e2e&gt;');
  expect(field.querySelector('[data-chips-input]').value).toBe('');

  // an unticked entry typed again is ticked again, not repeated
  build.checked = false;
  field.querySelector('[data-chips-input]').value = 'build';
  addChips(field);
  expect(build.checked).toBe(true);
  expect(field.querySelectorAll('input[value=build]')).toHaveLength(1);
});

test('initAscPolicyForm shows the buttons and wires Add, Enter and the stepper once', () => {
  document.body.innerHTML = form;
  initAscPolicyForm();
  initAscPolicyForm();
  const add = document.querySelector('[data-chips-add]');
  expect(add.hidden).toBe(false);
  expect(Array.from(document.querySelectorAll('[data-step]')).every((b) => !b.hidden)).toBe(true);

  const input = document.querySelector('[data-chips-input]');
  input.value = 'unit';
  add.click();
  input.value = 'docs';
  const enter = new KeyboardEvent('keydown', {key: 'Enter', cancelable: true});
  input.dispatchEvent(enter);
  expect(enter.defaultPrevented).toBe(true);
  expect(Array.from(document.querySelectorAll('input[name=checks]'), (b) => b.value)).toEqual(['build', 'unit', 'docs']);

  // one click moves one step: the buttons are wired once
  document.querySelector('[data-step="-1"]').click();
  expect(document.querySelector('input[name=approvals]').value).toBe('1');
});
