import {undoFragment} from './asc-ops.js';

const page = (inner) => new DOMParser().parseFromString(`<html><body><div data-testid="asc-undo">${inner}</div></body></html>`, 'text/html');

test('the fragment is what follows the header of the confirmation page', () => {
  const nodes = undoFragment(page('<header><h1>Undo</h1></header><p class="rv-lead">Check</p><form data-testid="asc-undo-form"></form>'));
  expect(nodes).toHaveLength(2);
  expect(nodes.map((n) => n.tagName)).toEqual(['P', 'FORM']);
});

test('a page without the confirmation, or with nothing in it, gives no fragment', () => {
  expect(undoFragment(new DOMParser().parseFromString('<html><body><p>Not found</p></body></html>', 'text/html'))).toBeNull();
  expect(undoFragment(page('<header></header>'))).toBeNull();
});
