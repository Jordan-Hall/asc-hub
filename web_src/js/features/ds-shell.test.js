import {cycleScope, parseRecents, pushRecent, visitFromPath} from './ds-shell.js';

test('Tab cycles the palette scope and wraps both ways', () => {
  expect(cycleScope('all')).toBe('changes');
  expect(cycleScope('commands')).toBe('all');
  expect(cycleScope('all', true)).toBe('commands');
  expect(cycleScope('nonsense')).toBe('changes');
});

test('saved recents drop anything that is not a path on this host', () => {
  const raw = JSON.stringify([
    {href: '/acme/payments', label: 'acme/payments'},
    {href: '//evil.example/x', label: 'x'},
    {href: 'https://evil.example', label: 'x'},
    {href: '/a/b/pulls/3', label: 'Wire retry', sub: 'a/b #3', kind: 'change'},
    {href: '/a/c'},
  ]);
  expect(parseRecents(raw).map((r) => r.href)).toEqual(['/acme/payments', '/a/b/pulls/3']);
  expect(parseRecents('not json')).toEqual([]);
  expect(parseRecents(null)).toEqual([]);
});

test('a new recent goes first, once, and the list stays short', () => {
  let list = [];
  for (const n of [1, 2, 3, 4, 5, 6]) list = pushRecent(list, {href: `/o/r${n}`, label: `r${n}`});
  list = pushRecent(list, {href: '/o/r4', label: 'r4'});
  expect(list.map((r) => r.href)).toEqual(['/o/r4', '/o/r6', '/o/r5', '/o/r3', '/o/r2']);
});

test('only a repository home or one change is a visit', () => {
  expect(visitFromPath('/acme/payments')).toMatchObject({kind: 'repo', href: '/acme/payments'});
  expect(visitFromPath('/acme/payments/pulls/12')).toMatchObject({kind: 'change', number: '12'});
  expect(visitFromPath('/sub/acme/payments/', '/sub')).toMatchObject({kind: 'repo', href: '/sub/acme/payments'});
  expect(visitFromPath('/acme/payments/src/branch/main')).toBeNull();
  expect(visitFromPath('/user/settings')).toBeNull();
  expect(visitFromPath('/-/asc')).toBeNull();
});

test('live results: a query needs two characters and at most eighty', async () => {
  const {liveQuery} = await import('./ds-shell.js');
  expect(liveQuery('a')).toBe('');
  expect(liveQuery(' ab ')).toBe('ab');
  expect(liveQuery('x'.repeat(81))).toBe('');
  expect(liveQuery(null)).toBe('');
});

test('live results: groups become options, a link that leaves the site is dropped', async () => {
  const {renderLive} = await import('./ds-shell.js');
  document.body.innerHTML = '<div id="ds-palette"><div data-ds-palette-list><div data-ds-palette-dynamic></div></div></div>';
  const list = document.querySelector('[data-ds-palette-list]');
  const p = {list, dynamic: list.querySelector('[data-ds-palette-dynamic]')};
  renderLive(p, [
    {key: 'changes', label: 'Changes', items: [{title: 'Add fee', sub: 'acme/pay #3', href: '/acme/pay/pulls/3'}, {title: 'Evil', href: 'https://evil.example/x'}, {title: 'Evil2', href: '//evil.example'}, {title: 'js', href: `${'java'}script:alert(1)`}]},
    {key: 'unknown', label: 'X', items: [{title: 'x', href: '/x'}]},
    {key: 'people', label: 'People', items: []},
  ]);
  const items = list.querySelectorAll('.ds-palette-item');
  expect(items).toHaveLength(1);
  expect(items[0].getAttribute('href')).toBe('/acme/pay/pulls/3');
  expect(items[0].getAttribute('role')).toBe('option');
  expect(items[0].querySelector('small').textContent).toBe('acme/pay #3');
  expect(list.querySelectorAll('.ds-palette-group')).toHaveLength(1);
});

test('edge swipe: from the left edge opens the drawer, a swipe left closes it, a scroll does nothing', async () => {
  const {edgeSwipe} = await import('./ds-shell.js');
  expect(edgeSwipe({x: 8, y: 300}, {x: 120, y: 310}, false)).toBe('open');
  expect(edgeSwipe({x: 200, y: 300}, {x: 320, y: 300}, false)).toBeNull();
  expect(edgeSwipe({x: 8, y: 300}, {x: 40, y: 300}, false)).toBeNull();
  expect(edgeSwipe({x: 8, y: 300}, {x: 120, y: 420}, false)).toBeNull();
  expect(edgeSwipe({x: 250, y: 300}, {x: 120, y: 310}, true)).toBe('close');
  expect(edgeSwipe({x: 120, y: 300}, {x: 250, y: 310}, true)).toBeNull();
});
