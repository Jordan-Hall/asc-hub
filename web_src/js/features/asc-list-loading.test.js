import {hideListLoading, initAscListLoading, showListLoading} from './asc-list-loading.js';

const stop = (e) => e.preventDefault();

const page = `<div class="rv-agents">
  <a id="chip" href="?state=failed">Failed</a>
  <form class="ag-search" method="get" action=""><button>Go</button></form>
  <div class="ag-main" data-asc-list-main>
    <div class="ag-loading" data-asc-list-loading hidden></div>
    <div class="rv-card" id="runs"></div>
  </div>
  <a id="run" href="/acme/nova/asc/agents/runs/4">Run 4</a>
</div>`;

test('loading shows the skeleton card once, and a restored page gets the list back', () => {
  document.body.innerHTML = page;
  const main = document.querySelector('[data-asc-list-main]');
  const card = main.querySelector('[data-asc-list-loading]');
  showListLoading(main);
  showListLoading(main);
  expect(main.classList.contains('is-loading')).toBe(true);
  expect(card.hidden).toBe(false);
  hideListLoading(main);
  expect(main.classList.contains('is-loading')).toBe(false);
  expect(card.hidden).toBe(true);
});

test('a chip or the filter form shows the skeleton; a run link does not', () => {
  document.body.innerHTML = page;
  initAscListLoading();
  const main = document.querySelector('[data-asc-list-main]');
  document.addEventListener('click', stop);
  document.querySelector('#run').dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, button: 0}));
  expect(main.classList.contains('is-loading')).toBe(false);
  document.querySelector('#chip').dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, button: 0}));
  expect(main.classList.contains('is-loading')).toBe(true);
  hideListLoading(main);
  document.querySelector('form').dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}));
  expect(main.classList.contains('is-loading')).toBe(true);
  document.removeEventListener('click', stop);
});

test('the filter form of the operation log shows the skeleton; another form does not', () => {
  document.body.innerHTML = `<form id="other" method="post" action=""><button>Go</button></form>
  <form id="filter" class="rv-filters" method="get" action="" data-asc-list-filter><button>Filter</button></form>
  <div data-asc-list-main><div data-asc-list-loading hidden></div><section id="ops"></section></div>`;
  initAscListLoading();
  const main = document.querySelector('[data-asc-list-main]');
  document.querySelector('#other').dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}));
  expect(main.classList.contains('is-loading')).toBe(false);
  document.querySelector('#filter').dispatchEvent(new Event('submit', {bubbles: true, cancelable: true}));
  expect(main.classList.contains('is-loading')).toBe(true);
});

test('a page without the runs list is left alone', () => {
  document.body.innerHTML = '<p>other</p>';
  expect(() => initAscListLoading()).not.toThrow();
});
