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
