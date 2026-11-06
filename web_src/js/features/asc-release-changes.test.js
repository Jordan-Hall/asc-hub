import {initAscReleaseChanges, showCategory} from './asc-release-changes.js';

function page() {
  document.body.innerHTML = `<details data-rel-changes open><summary>3 changes</summary>
    <div class="rp-rel-filter">
      <button type="button" data-rel-cat="" aria-pressed="true">All 3</button>
      <button type="button" data-rel-cat="added" aria-pressed="false">Added 2</button>
      <button type="button" data-rel-cat="fixed" aria-pressed="false">Fixed 1</button>
    </div>
    <ul class="rp-rel-list">
      <li data-rel-cat="added">a</li><li data-rel-cat="fixed">b</li><li data-rel-cat="added">c</li>
    </ul></details>`;
  initAscReleaseChanges();
}

const shown = () => Array.from(document.querySelectorAll('li')).filter((li) => !li.hidden).map((li) => li.textContent);
const pressed = () => Array.from(document.querySelectorAll('button'), (b) => b.getAttribute('aria-pressed'));

test('a chip shows the changes of its category and All shows every change', () => {
  page();
  document.querySelector('button[data-rel-cat="fixed"]').click();
  expect(shown()).toEqual(['b']);
  expect(pressed()).toEqual(['false', 'false', 'true']);
  document.querySelector('button[data-rel-cat="added"]').click();
  expect(shown()).toEqual(['a', 'c']);
  document.querySelector('button[data-rel-cat=""]').click();
  expect(shown()).toEqual(['a', 'b', 'c']);
  expect(pressed()).toEqual(['true', 'false', 'false']);
});

test('showCategory with no category shows every change', () => {
  page();
  const list = document.querySelector('ul');
  showCategory(list, 'fixed');
  showCategory(list, '');
  expect(shown()).toEqual(['a', 'b', 'c']);
});
