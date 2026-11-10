import {invertFileFolding, setFileFolding} from './file-fold.js';

test('the fold arrow says whether the file is shown', () => {
  document.body.innerHTML = `<div class="file-content"><button class="fold-file" aria-expanded="true"></button><div class="diff-file-body"></div></div>`;
  const box = document.querySelector('.file-content');
  const arrow = document.querySelector('.fold-file');

  setFileFolding(box, arrow, true);
  expect(box.getAttribute('data-folded')).toEqual('true');
  expect(arrow.getAttribute('aria-expanded')).toEqual('false');

  invertFileFolding(box, arrow);
  expect(box.getAttribute('data-folded')).toEqual('false');
  expect(arrow.getAttribute('aria-expanded')).toEqual('true');
});
