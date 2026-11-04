import {lineAnchor} from './asc-goto-line.js';

test('the number typed becomes a line anchor', () => {
  document.body.innerHTML = `<table>
    <tr><td class="lines-num"><span id="L1"></span></td></tr>
    <tr><td class="lines-num"><span id="L2"></span></td></tr>
    <tr><td class="lines-num"><span id="L3"></span></td></tr>
  </table><p id="L9">not a line</p>`;
  expect(lineAnchor('2')).toBe('L2');
  expect(lineAnchor(' 3 ')).toBe('L3');
  expect(lineAnchor('40')).toBe('L3');
  expect(lineAnchor('9')).toBe('L3');
  expect(lineAnchor('0')).toBe('');
  expect(lineAnchor('-1')).toBe('');
  expect(lineAnchor('1.5')).toBe('');
  expect(lineAnchor('L2')).toBe('');
  expect(lineAnchor('')).toBe('');
});

test('a page without lines has no anchor', () => {
  document.body.innerHTML = '<p>image</p>';
  expect(lineAnchor('4')).toBe('');
});
