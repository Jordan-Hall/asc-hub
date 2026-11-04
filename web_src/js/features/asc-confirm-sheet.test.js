import {confirmFragment, formURL, initAscConfirmSheet, undoToastOf} from './asc-confirm-sheet.js';
import {GET} from '../modules/fetch.js';

vi.mock('../modules/fetch.js', () => ({GET: vi.fn()}));

const page = `<div data-testid="asc-confirm"><header class="rv-head"><h1 class="rv-title"> Merge 2 changes into main? </h1></header>
<p class="rv-lead">lands now</p>
<form class="asc-land-scope" method="get" action="/o/r/pulls/5/asc/confirm"><input type="hidden" name="op" value="merge"><input type="radio" name="to" value="4" checked></form>
<form class="asc-op-form" method="post" action="/o/r/pulls/5/asc/merge"><div class="asc-op-buttons"><button data-testid="asc-confirm-button">Merge 2 changes</button><a class="rv-btn rv-btn-secondary" href="/o/r/pulls/5" autofocus data-testid="asc-confirm-cancel">Cancel</a></div></form></div>`;

const parse = (html) => new DOMParser().parseFromString(html, 'text/html');

function win(matches) {
  return {matchMedia: () => ({matches, addEventListener: vi.fn()}), location: {href: 'https://hub.example.org/o/r/pulls/5', assign: vi.fn()}};
}

test('confirmFragment takes the title and the parts below the header', () => {
  const part = confirmFragment(parse(page));
  expect(part.title).toBe('Merge 2 changes into main?');
  expect(part.nodes.map((n) => n.tagName)).toEqual(['P', 'FORM', 'FORM']);
  expect(confirmFragment(parse('<p>Sign in</p>'))).toBeNull();
});

test('formURL carries the fields of a GET form', () => {
  const form = parse(page).querySelector('form.asc-land-scope');
  expect(formURL(form, 'https://hub.example.org/o/r/pulls/5')).toBe('https://hub.example.org/o/r/pulls/5/asc/confirm?op=merge&to=4');
});

test('undoToastOf splits the message from its Undo link', () => {
  const flash = parse('<div id="flash-message" class="flash-success"><p>Merged 2 pull requests. Undo stays open. <a href="/o/r/asc/ops/undo?ref=x&amp;seq=3">Undo</a></p></div>').body.firstChild;
  expect(undoToastOf(flash)).toEqual({text: 'Merged 2 pull requests. Undo stays open.', label: 'Undo', href: expect.stringContaining('/o/r/asc/ops/undo?ref=x&seq=3')});
  expect(undoToastOf(parse('<div><p>Saved.</p></div>').body.firstChild)).toBeNull();
});

describe('initAscConfirmSheet', () => {
  beforeEach(() => {
    document.body.innerHTML = `<a data-asc-confirm-open href="/o/r/pulls/5/asc/confirm?op=merge">Merge</a>
<dialog data-asc-confirm-sheet><h2 class="rv-sheet-h"></h2><div data-asc-confirm-sheet-body></div></dialog>`;
    const sheet = document.querySelector('dialog');
    sheet.showModal = vi.fn(() => sheet.setAttribute('open', ''));
    sheet.close = vi.fn(() => {
      sheet.removeAttribute('open');
      sheet.dispatchEvent(new Event('close'));
    });
    GET.mockReset();
  });

  test('on a phone the confirmation opens in the sheet with the focus on Cancel', async () => {
    GET.mockResolvedValue({ok: true, text: () => Promise.resolve(page)});
    initAscConfirmSheet(document, win(true));
    document.querySelector('a').click();
    await vi.waitFor(() => expect(document.querySelector('dialog').showModal).toHaveBeenCalled());
    expect(document.querySelector('.rv-sheet-h').textContent).toBe('Merge 2 changes into main?');
    expect(document.activeElement.dataset.testid).toBe('asc-confirm-cancel');
    expect(document.querySelector('[data-asc-confirm-sheet-body] [autofocus]')).toBeNull();
    document.querySelector('[data-testid="asc-confirm-cancel"]').click();
    expect(document.querySelector('dialog').close).toHaveBeenCalled();
    expect(document.querySelector('[data-asc-confirm-sheet-body]').children).toHaveLength(0);
  });

  test('a wider screen follows the link', () => {
    const doc = document.implementation.createHTMLDocument(); // the listeners of the phone tests stay on document
    doc.body.innerHTML = document.body.innerHTML;
    initAscConfirmSheet(doc, win(false));
    const e = new MouseEvent('click', {bubbles: true, cancelable: true});
    doc.querySelector('a').dispatchEvent(e);
    expect(e.defaultPrevented).toBe(false);
    expect(GET).not.toHaveBeenCalled();
  });

  test('a refusal opens the page', async () => {
    GET.mockResolvedValue({ok: false, text: () => Promise.resolve('')});
    const w = win(true);
    initAscConfirmSheet(document, w);
    document.querySelector('a').click();
    await vi.waitFor(() => expect(w.location.assign).toHaveBeenCalledWith(expect.stringContaining('/asc/confirm?op=merge')));
  });
});
