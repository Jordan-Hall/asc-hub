import {initAscDocsToc} from './asc-docs-toc.js';

test('a tap on a heading link closes the list', () => {
  document.body.innerHTML = '<details class="site-toc-m" open><summary>On this page</summary><ul><li><a href="#a">A</a></li></ul></details>';
  initAscDocsToc();
  const toc = document.querySelector('details');
  expect(toc.open).toBe(true);
  toc.querySelector('a').click();
  expect(toc.open).toBe(false);
});
