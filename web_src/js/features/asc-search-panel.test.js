import {initAscSearchPanel, panelNode} from './asc-search-panel.js';

vi.mock('../modules/fetch.js', () => ({
  GET: vi.fn(async (url) => ({
    text: async () => (url.includes('bad') ? '<p>nothing</p>' : `<div class="asc-panel"><h2>${new URL(url, 'https://x.test').searchParams.get('name')}</h2></div>`),
  })),
}));

function page(wide) {
  window.matchMedia = vi.fn(() => ({matches: wide}));
  document.body.innerHTML = `<main>
    <a id="a" href="/o/r/asc/symbols?op=def&name=Parse" data-search-panel="/o/r/asc/search/panel?name=Parse">Parse</a>
    <a id="b" href="/o/r/asc/symbols?op=def&name=bad" data-search-panel="/o/r/asc/search/panel?name=bad">bad</a>
    <aside id="asc-search-panel" data-loading="Loading" data-failed="Failed"><p>hint</p></aside>
  </main>`;
  initAscSearchPanel(document);
}

const flush = () => new Promise((r) => setTimeout(r, 0));

test('a fragment yields its panel element', () => {
  expect(panelNode('<div class="asc-panel"><h2>x</h2></div>').querySelector('h2').textContent).toBe('x');
  expect(panelNode('<p>none</p>')).toBeNull();
});

test('a wide screen loads the panel in place', async () => {
  page(true);
  const a = document.querySelector('#a');
  const e = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0});
  a.dispatchEvent(e);
  expect(e.defaultPrevented).toBe(true);
  expect(document.querySelector('#asc-search-panel').textContent).toBe('Loading');
  await flush();
  const aside = document.querySelector('#asc-search-panel');
  expect(aside.querySelector('.asc-panel h2').textContent).toBe('Parse');
  expect(aside.hasAttribute('aria-busy')).toBe(false);
  expect(a.getAttribute('aria-current')).toBe('true');
});

test('a fragment without a panel shows the failure', async () => {
  page(true);
  document.querySelector('#b').dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, button: 0}));
  await flush();
  expect(document.querySelector('#asc-search-panel').textContent).toBe('Failed');
});

test('a narrow screen and a modified click follow the link', () => {
  page(false);
  const e = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0});
  document.querySelector('#a').dispatchEvent(e);
  expect(e.defaultPrevented).toBe(false);
  page(true);
  const m = new MouseEvent('click', {bubbles: true, cancelable: true, button: 0, ctrlKey: true});
  document.querySelector('#a').dispatchEvent(m);
  expect(m.defaultPrevented).toBe(false);
});
