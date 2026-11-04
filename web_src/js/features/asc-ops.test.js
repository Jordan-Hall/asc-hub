import {GET} from '../modules/fetch.js';
import {initAscOps, undoFragment, undoLinkOf} from './asc-ops.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

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

const tick = () => new Promise((r) => setTimeout(r, 0));

describe('initAscOps', () => {
  const undoPage = '<html><body><div data-testid="asc-undo"><header><h1>Undo</h1></header><p class="rv-lead">Check</p>' +
    '<form data-testid="asc-undo-form"><input name="reason"><div class="rv-sticky"><button type="submit">Undo</button>' +
    '<a class="rv-btn rv-btn-secondary" href="/o/r/asc/ops">Cancel</a></div></form></div></body></html>';

  const setup = (isPhone) => {
    window.matchMedia = vi.fn(() => ({matches: isPhone, addEventListener: vi.fn()}));
    document.body.innerHTML = `
      <ol data-testid="asc-ops-list"><li class="rv-op is-selectable"><code id="hash">abc</code>
        <a href="/o/r/asc/ops/undo?ref=a&amp;seq=2" data-testid="asc-op-undo">Undo</a></li></ol>
      <section data-asc-ops-panel><div data-asc-ops-panel-body><p id="idle">Pick one</p></div></section>
      <dialog data-asc-ops-sheet><h2>Undo this operation?</h2><div data-asc-ops-sheet-body></div></dialog>`;
    const sheet = document.querySelector('dialog');
    sheet.showModal = vi.fn(function () { this.setAttribute('open', '') });
    sheet.close = vi.fn(function () { this.removeAttribute('open'); this.dispatchEvent(new Event('close')) });
    Element.prototype.scrollIntoView = vi.fn();
    vi.mocked(GET).mockResolvedValue({ok: true, text: () => Promise.resolve(undoPage)});
    initAscOps();
    return sheet;
  };

  test('a click on the row loads the preview into the side panel and marks the row', async () => {
    const sheet = setup(false);
    document.querySelector('#hash').click();
    await tick();
    expect(GET).toHaveBeenCalled();
    expect(document.querySelector('[data-asc-ops-panel-body] [data-testid="asc-undo-form"]')).not.toBeNull();
    expect(document.querySelector('.rv-op').classList.contains('is-selected')).toBe(true);
    expect(sheet.showModal).not.toHaveBeenCalled();
    document.querySelector('[data-asc-ops-panel-body] a.rv-btn-secondary').click();
    expect(document.querySelector('#idle')).not.toBeNull();
    expect(document.querySelector('.rv-op').classList.contains('is-selected')).toBe(false);
  });

  test('on a phone the preview opens as a sheet, focuses Cancel and Cancel closes it', async () => {
    const sheet = setup(true);
    document.querySelector('[data-testid="asc-op-undo"]').click();
    await tick();
    expect(sheet.showModal).toHaveBeenCalled();
    expect(sheet.querySelector('[data-testid="asc-undo-form"]')).not.toBeNull();
    expect(document.querySelector('#idle')).not.toBeNull();
    const cancel = sheet.querySelector('a.rv-btn-secondary');
    expect(document.activeElement).toBe(cancel);
    cancel.click();
    expect(sheet.hasAttribute('open')).toBe(false);
    expect(sheet.querySelector('[data-testid="asc-undo-form"]')).toBeNull();
    expect(document.querySelector('.rv-op').classList.contains('is-selected')).toBe(false);
  });
});
