import {initMarkupAnchors} from './anchors.js';

test('a heading anchor is a named link', () => {
  document.body.innerHTML = '<div class="markup"><h2 id="user-content-run-it">Run it locally</h2></div>';
  initMarkupAnchors();
  const a = document.querySelector('h2 > a.anchor');
  expect(a.getAttribute('href')).toBe('#run-it');
  expect(a.getAttribute('aria-label')).toBe('Run it locally');
});
