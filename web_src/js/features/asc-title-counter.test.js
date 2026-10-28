import {countChars, describeCount, updateCounter, initAscTitleCounter} from './asc-title-counter.js';

test('countChars counts characters, not code units', () => {
  expect(countChars('abc')).toBe(3);
  expect(countChars('a😀b')).toBe(3);
  expect(countChars(null)).toBe(0);
});

test('describeCount fills the template', () => {
  expect(describeCount(68, 120, '%d of %d')).toBe('68 of 120');
  expect(describeCount(1, 2)).toBe('1 of 2');
});

function page(value) {
  document.body.innerHTML = `<input id="t" value="${value}" data-asc-count="o" data-asc-count-max="10" data-asc-count-template="%d of %d" data-asc-count-over="Too long.">` + '<span id="o"></span>';
  return [document.querySelector('#t'), document.querySelector('#o')];
}

test('the counter shows N of max and flags a title over the advice', () => {
  const [input, out] = page('Add retry');
  expect(updateCounter(input, out)).toEqual({n: 9, max: 10, over: false});
  expect(out.textContent).toBe('9 of 10');
  expect(input.getAttribute('aria-invalid')).toBe('false');
  input.value = 'Add retry policy';
  expect(updateCounter(input, out).over).toBe(true);
  expect(out.textContent).toBe('16 of 10. Too long.');
  expect(input.getAttribute('aria-invalid')).toBe('true');
  expect(out.classList.contains('ds-err-msg')).toBe(true);
});

test('init binds typing to the counter', () => {
  const [input, out] = page('a');
  initAscTitleCounter();
  expect(out.textContent).toBe('1 of 10');
  input.value = 'abcd';
  input.dispatchEvent(new Event('input'));
  expect(out.textContent).toBe('4 of 10');
});
