import {dateURL, initAscHistorySheet, openPart, pathSegments, pathURL, stackURL} from './asc-history-sheet.js';
import {CLOSE_PX, dragOutcome} from './asc-sheet.js';

test('a path becomes escaped segments without empty, "." or ".." parts', () => {
  expect(pathSegments('src/a b.go')).toBe('src/a%20b.go');
  expect(pathSegments('/src//../x/./y/')).toBe('src/x/y');
  expect(pathSegments('a?b#c')).toBe('a%3Fb%23c');
  expect(pathSegments('')).toBe('');
});

test('Path and Stack go to the history of that path or branch', () => {
  expect(pathURL('/o/r', 'branch/main', 'src/x')).toBe('/o/r/commits/branch/main/src/x');
  expect(pathURL('/o/r', 'branch/main', '  ')).toBe('/o/r/commits/branch/main/%20%20');
  expect(pathURL('/o/r', 'branch/main', '')).toBe('/o/r/commits/branch/main');
  expect(stackURL('/o/r', 'feat/one', '')).toBe('/o/r/commits/branch/feat/one');
  expect(stackURL('/o/r', 'feat/one', 'docs')).toBe('/o/r/commits/branch/feat/one/docs');
  expect(stackURL('/o/r', '', 'docs')).toBe('');
});

test('Date searches with after: and before: and keeps the other words', () => {
  expect(dateURL('/o/r', 'branch/main', '2026-01-02', '', '')).toBe('/o/r/commits/branch/main/search?q=after%3A2026-01-02');
  expect(dateURL('/o/r', 'branch/main', '2026-01-02', '2026-02-01', 'fix author:ada')).toBe(
    '/o/r/commits/branch/main/search?q=after%3A2026-01-02%20before%3A2026-02-01%20fix%20author%3Aada',
  );
  expect(dateURL('/o/r', 'branch/main', 'x', '2026-13', '')).toBe('/o/r/commits/branch/main');
  expect(dateURL('/o/r', 'branch/main', '', '', ' parse ')).toBe('/o/r/commits/branch/main/search?q=parse');
});

const page = `<button data-history-chip="path">Path</button><button data-history-chip="date">Date</button>
<dialog data-history-sheet data-repo-link="/o/r" data-ref-sub="branch/main" data-tree-path="" data-words="fix">
  <h2 data-history-sheet-h></h2>
  <div data-history-part="who" data-title="Who" hidden></div>
  <form data-history-part="path" data-title="Path" hidden><input name="path" value="src"><button>Go</button></form>
  <form data-history-part="date" data-title="Date" hidden><input name="after" value="2026-03-04"><input name="before" value=""><button>Go</button></form>
</dialog>`;

function mount() {
  document.body.innerHTML = page;
  const dialog = document.querySelector('dialog');
  dialog.showModal = function () {
    this.open = true;
  };
  return dialog;
}

test('a chip opens its part of the sheet only', () => {
  const dialog = mount();
  expect(openPart(dialog, 'date')).toBe(true);
  expect(dialog.open).toBe(true);
  expect(dialog.querySelector('[data-history-sheet-h]').textContent).toBe('Date');
  expect(dialog.querySelector('[data-history-part="date"]').hidden).toBe(false);
  expect(dialog.querySelector('[data-history-part="path"]').hidden).toBe(true);
  expect(openPart(mount(), 'nope')).toBe(false);
});

test('submitting a part goes to its page', () => {
  const dialog = mount();
  const assign = vi.fn();
  const real = window.location;
  Object.defineProperty(window, 'location', {configurable: true, value: {...real, assign}});
  initAscHistorySheet();
  document.querySelector('[data-history-chip="path"]').click();
  expect(dialog.querySelector('[data-history-part="path"]').hidden).toBe(false);
  dialog.querySelector('form[data-history-part="path"]').requestSubmit();
  dialog.querySelector('form[data-history-part="date"]').requestSubmit();
  Object.defineProperty(window, 'location', {configurable: true, value: real});
  expect(assign.mock.calls).toEqual([['/o/r/commits/branch/main/src'], ['/o/r/commits/branch/main/search?q=after%3A2026-03-04%20fix']]);
});

test('a long drag down closes a sheet, a short one springs back', () => {
  expect(dragOutcome(CLOSE_PX + 1)).toBe('close');
  expect(dragOutcome(CLOSE_PX)).toBe('stay');
  expect(dragOutcome(-40)).toBe('stay');
});
