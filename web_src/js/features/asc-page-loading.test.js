import {initAscPageLoading, leavesForPage, setPageLoading} from './asc-page-loading.js';

const loc = new URL('https://example.org/acme/ledger/src/branch/main/a.rs');

function link(href, attrs = {}) {
  const a = document.createElement('a');
  a.href = href;
  for (const [k, v] of Object.entries(attrs)) a.setAttribute(k, v);
  return a;
}

// on the window, after the page's listener: jsdom does not navigate
const stay = (e) => e.preventDefault();

const click = (more = {}) => ({defaultPrevented: false, button: 0, metaKey: false, ctrlKey: false, shiftKey: false, altKey: false, ...more});

test('leavesForPage: a plain click on another page of this site', () => {
  expect(leavesForPage(click(), link('https://example.org/acme/ledger/src/branch/main/b.rs'), loc)).toBe(true);
  expect(leavesForPage(click(), link('https://example.org/acme/ledger/src/branch/main/a.rs?who=agents'), loc)).toBe(true);
  expect(leavesForPage(click({ctrlKey: true}), link('https://example.org/acme/ledger/src/branch/main/b.rs'), loc)).toBe(false);
  expect(leavesForPage(click({button: 1}), link('https://example.org/acme/ledger/src/branch/main/b.rs'), loc)).toBe(false);
  expect(leavesForPage(click(), link('https://example.org/acme/ledger/src/branch/main/a.rs#L3'), loc)).toBe(false);
  expect(leavesForPage(click(), link('https://other.example/x'), loc)).toBe(false);
  expect(leavesForPage(click(), link('https://example.org/x', {target: '_blank'}), loc)).toBe(false);
  expect(leavesForPage(click(), link('https://example.org/x', {'aria-current': 'page'}), loc)).toBe(false);
});

test('the skeleton takes the place of the content and gives it back', () => {
  document.body.innerHTML = `
    <nav data-loading-links><a href="/elsewhere">b.rs</a><a href="/y" aria-current="page">a.rs</a></nav>
    <a class="outside" href="/commit/1">a commit</a>
    <form data-loading-form><button>Search</button></form>
    <div data-loading-body>code</div>
    <div data-loading-skel hidden></div>`;
  const body = document.querySelector('[data-loading-body]');
  const skeleton = document.querySelector('[data-loading-skel]');
  initAscPageLoading();
  window.addEventListener('click', stay);

  document.querySelector('[aria-current]').click();
  document.querySelector('.outside').click();
  expect(skeleton.hidden).toBe(true);

  document.querySelector('a[href="/elsewhere"]').click();
  expect(skeleton.hidden).toBe(false);
  expect(body.hidden).toBe(true);

  window.dispatchEvent(new Event('pageshow'));
  expect(skeleton.hidden).toBe(true);
  expect(body.hidden).toBe(false);

  const form = document.querySelector('form');
  form.addEventListener('submit', stay);
  form.dispatchEvent(new Event('submit', {bubbles: true}));
  expect(skeleton.hidden).toBe(false);
  expect(setPageLoading(true, document.createElement('div'))).toBe(false);
  window.removeEventListener('click', stay);
});
