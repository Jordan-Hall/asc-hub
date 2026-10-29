import {pickActive} from './asc-docs.js';

test('the section in view is the last heading that has reached the top', () => {
  const heads = [{id: 'a', top: -400}, {id: 'b', top: 20}, {id: 'c', top: 500}];
  expect(pickActive(heads)).toBe('b');
  expect(pickActive([{id: 'a', top: 300}, {id: 'b', top: 900}])).toBe('a');
  expect(pickActive([{id: 'a', top: -10}, {id: 'b', top: -5}])).toBe('b');
  expect(pickActive([])).toBeNull();
});
