import {undoFragment, undoLinkOf} from './asc-ops.js';

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

describe('undoLinkOf', () => {
  const doc = new DOMParser().parseFromString(`<html><body>
    <div data-testid="asc-ops-restack"><a href="/o/r/asc/ops/undo?ref=a&amp;seq=2" data-asc-ops-load>Undo restack</a><a href="/o/r/asc/compare">See</a></div>
    <ol data-testid="asc-ops-list">
      <li class="rv-op is-selectable" id="undoable"><span class="rv-op-title">Restack</span><code>abc</code><a href="/o/r/asc/ops/undo?ref=a&amp;seq=2" data-testid="asc-op-undo">Undo</a><a href="/o/r/asc/changes/1" id="other">Change</a></li>
      <li class="rv-op" id="fixed"><span class="rv-op-title">Land</span></li>
    </ol></body></html>`, 'text/html');
  const q = (s) => doc.querySelector(s);

  test('a click anywhere on a row that can be undone selects its Undo', () => {
    expect(undoLinkOf(q('#undoable code'))).toBe(q('#undoable [data-testid="asc-op-undo"]'));
    expect(undoLinkOf(q('#undoable [data-testid="asc-op-undo"]'))).toBe(q('#undoable [data-testid="asc-op-undo"]'));
  });

  test('Undo restack in the banner loads in the panel too', () => {
    expect(undoLinkOf(q('[data-asc-ops-load]'))).toBe(q('[data-asc-ops-load]'));
  });

  test('other links, rows without Undo and a text selection select nothing', () => {
    expect(undoLinkOf(q('#other'))).toBeNull();
    expect(undoLinkOf(q('#fixed .rv-op-title'))).toBeNull();
    expect(undoLinkOf(q('#undoable code'), 'abc')).toBeNull();
    expect(undoLinkOf(q('[data-testid="asc-ops-restack"] a:not([data-asc-ops-load])'))).toBeNull();
  });
});
