import {digitsOf, initAscCodeBoxes} from './asc-codeboxes.js';

test('digitsOf keeps six digits and nothing else', () => {
  expect(digitsOf('123 456')).toBe('123456');
  expect(digitsOf('12-34-56-78')).toBe('123456');
  expect(digitsOf('abc')).toBe('');
  expect(digitsOf(null)).toBe('');
});

function page() {
  document.body.innerHTML = `<form><div class="ds-field"><label for="passcode">Code</label><input id="passcode" name="passcode" data-code-field required></div></form>`;
  const form = document.querySelector('form');
  form.requestSubmit = vi.fn();
  return form;
}

test('six boxes fill the field, move on, and send the form with the sixth digit', () => {
  const form = page();
  initAscCodeBoxes();
  const boxes = document.querySelectorAll('.ds-codebox');
  expect(boxes).toHaveLength(6);
  const field = document.querySelector('#passcode');
  expect(field.hidden).toBe(true);
  boxes[0].value = '1';
  boxes[0].dispatchEvent(new Event('input'));
  expect(field.value).toBe('1');
  expect(document.activeElement).toBe(boxes[1]);
  expect(form.requestSubmit).not.toHaveBeenCalled();

  // a paste fills all the boxes and sends
  const paste = new Event('paste', {cancelable: true});
  paste.clipboardData = {getData: () => '654 321'};
  boxes[0].dispatchEvent(paste);
  expect(field.value).toBe('654321');
  expect(form.requestSubmit).toHaveBeenCalledTimes(1);
});

test('backspace in an empty box clears and goes back', () => {
  page();
  initAscCodeBoxes();
  const boxes = document.querySelectorAll('.ds-codebox');
  boxes[0].value = '1';
  boxes[0].dispatchEvent(new Event('input'));
  boxes[1].dispatchEvent(new KeyboardEvent('keydown', {key: 'Backspace', cancelable: true}));
  expect(boxes[0].value).toBe('');
  expect(document.querySelector('#passcode').value).toBe('');
  expect(document.activeElement).toBe(boxes[0]);
});

test('nothing happens on a page without the field', () => {
  document.body.innerHTML = '<p>x</p>';
  expect(() => initAscCodeBoxes()).not.toThrow();
});
