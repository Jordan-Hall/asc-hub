import {splitKeys, readKeymap, resolveKey, chordMs, initAscRepoKeys, openRow, copyRowId, toggleTarget} from './asc-repo-keys.js';

afterEach(() => {
  document.body.innerHTML = '';
});

test('splitKeys reads one key or a sequence', () => {
  expect(splitKeys('t')).toEqual(['t']);
  expect(splitKeys(' g   c ')).toEqual(['g', 'c']);
  expect(splitKeys('')).toEqual([]);
  expect(splitKeys(null)).toEqual([]);
});

test('readKeymap keeps the first shown element of a key and skips hidden or disabled ones', () => {
  document.body.innerHTML = `
    <a id="a" href="/x" data-asc-key="t" hidden>a</a>
    <a id="b" href="/x" data-asc-key="t">b</a>
    <button id="c" data-asc-key="g c" disabled>c</button>
    <div hidden><a id="d" href="/x" data-asc-key="y">d</a></div>
    <a id="e" href="/x" data-asc-key="g  s">e</a>`;
  const map = readKeymap();
  expect(map.get('t').id).toBe('b');
  expect(map.has('g c')).toBe(false);
  expect(map.has('y')).toBe(false);
  expect(map.get('g s').id).toBe('e');
});

test('resolveKey fires a single key at once and a sequence on its second key within the window', () => {
  const seqs = new Set(['t', 'g c']);
  let r = resolveKey({pending: null, at: 0}, 't', 100, seqs);
  expect(r.seq).toBe('t');
  r = resolveKey({pending: null, at: 0}, 'g', 100, seqs);
  expect(r.seq).toBeNull();
  expect(r.state.pending).toBe('g');
  r = resolveKey(r.state, 'c', 100 + chordMs, seqs);
  expect(r.seq).toBe('g c');
  expect(r.state.pending).toBeNull();
});

test('resolveKey forgets a first key after the window and ignores unknown keys', () => {
  const seqs = new Set(['g c']);
  let r = resolveKey({pending: null, at: 0}, 'g', 100, seqs);
  r = resolveKey(r.state, 'c', 100 + chordMs + 1, seqs);
  expect(r.seq).toBeNull();
  r = resolveKey({pending: null, at: 0}, 'x', 5, seqs);
  expect(r.seq).toBeNull();
  expect(r.state.pending).toBeNull();
});

test('the handler clicks the declared control and ignores fields and modifiers', () => {
  document.body.innerHTML = '<a id="t" href="#find" data-asc-key="t">Go to file</a><input id="q">';
  const clicks = [];
  document.querySelector('#t').addEventListener('click', (e) => {
    e.preventDefault();
    clicks.push('t');
  });
  initAscRepoKeys();
  document.querySelector('#q').dispatchEvent(new KeyboardEvent('keydown', {key: 't', bubbles: true}));
  expect(clicks).toEqual([]);
  document.body.dispatchEvent(new KeyboardEvent('keydown', {key: 't', bubbles: true}));
  expect(clicks).toEqual(['t']);
  document.body.dispatchEvent(new KeyboardEvent('keydown', {key: 't', ctrlKey: true, bubbles: true}));
  expect(clicks).toEqual(['t']);
});

test('a row opens its link and copies its id', async () => {
  document.body.innerHTML = '<ul data-asc-list><li id="r" data-asc-row tabindex="-1" data-asc-id="wkqzr"><a href="#x">One</a><a data-asc-open href="#y">Open</a></li></ul>';
  const row = document.querySelector('#r');
  const opened = [];
  for (const a of row.querySelectorAll('a')) {
    a.addEventListener('click', (e) => {
      e.preventDefault();
      opened.push(a.getAttribute('href'));
    });
  }
  expect(openRow(row)).toBe(true);
  expect(opened).toEqual(['#y']);
  const written = [];
  Object.defineProperty(navigator, 'clipboard', {value: {writeText: async (v) => written.push(v)}, configurable: true});
  expect(await copyRowId(row)).toBe(true);
  expect(written).toEqual(['wkqzr']);
  expect(row.getAttribute('data-asc-copied')).toBe('true');
});

test('a toggle button switches the class of its target and aria-pressed', () => {
  document.body.innerHTML = '<div class="grid"></div><button id="b" data-asc-toggle="off" data-asc-toggle-target=".grid" aria-pressed="false">x</button><button id="n" data-asc-toggle="off" data-asc-toggle-target=".none">y</button>';
  const btn = document.querySelector('#b');
  expect(toggleTarget(btn)).toBe(true);
  expect(document.querySelector('.grid').classList.contains('off')).toBe(true);
  expect(btn.getAttribute('aria-pressed')).toBe('true');
  expect(toggleTarget(btn)).toBe(false);
  expect(btn.getAttribute('aria-pressed')).toBe('false');
  expect(toggleTarget(document.querySelector('#n'))).toBeNull();
});
