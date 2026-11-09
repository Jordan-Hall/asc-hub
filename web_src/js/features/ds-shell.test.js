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

test('live results: the commands of the page show in the Commands scope', async () => {
  const {renderLive, liveGroupScopes} = await import('./ds-shell.js');
  expect(liveGroupScopes.commands).toBe('commands');
  document.body.innerHTML = '<div id="ds-palette"><div data-ds-palette-list><div data-ds-palette-dynamic></div></div></div>';
  const list = document.querySelector('[data-ds-palette-list]');
  const p = {list, dynamic: list.querySelector('[data-ds-palette-dynamic]')};
  renderLive(p, [{key: 'commands', label: 'Commands', items: [{title: 'Undo last operation', sub: 'acme/pay', href: '/acme/pay/asc/ops/undo/last'}]}]);
  const item = list.querySelector('.ds-palette-item');
  expect(item.dataset.dsScope).toBe('commands');
  expect(item.getAttribute('href')).toBe('/acme/pay/asc/ops/undo/last');
  expect(list.querySelector('.ds-palette-group').textContent).toBe('Commands');
});

test('live results: symbols show in the Code scope with their path', async () => {
  const {renderLive, liveGroupScopes} = await import('./ds-shell.js');
  expect(liveGroupScopes.code).toBe('code');
  document.body.innerHTML = '<div id="ds-palette"><div data-ds-palette-list><div data-ds-palette-dynamic></div></div></div>';
  const list = document.querySelector('[data-ds-palette-list]');
  const p = {list, dynamic: list.querySelector('[data-ds-palette-dynamic]')};
  renderLive(p, [{key: 'code', label: 'Code', items: [{title: 'RetryPolicy', sub: 'struct · acme/pay · src/retry.rs:4', href: '/acme/pay/src/branch/master/src/retry.rs#L4'}]}]);
  const item = list.querySelector('.ds-palette-item');
  expect(item.dataset.dsScope).toBe('code');
  expect(item.getAttribute('href')).toBe('/acme/pay/src/branch/master/src/retry.rs#L4');
  expect(item.querySelector('small').textContent).toBe('struct · acme/pay · src/retry.rs:4');
  expect(list.querySelector('.ds-palette-group').textContent).toBe('Code');
});

test('live url: the query and the page the palette is open on', async () => {
  const {liveUrl} = await import('./ds-shell.js');
  expect(liveUrl('/-/asc/palette', 'undo x', '/acme/pay/pulls/3')).toBe('/-/asc/palette?q=undo+x&at=%2Facme%2Fpay%2Fpulls%2F3');
});

test('verb scope: a verb switches to Commands and back, a chosen scope stays', async () => {
  const {verbScope} = await import('./ds-shell.js');
  expect(verbScope('all', false, true)).toBe('commands');
  expect(verbScope('changes', false, true)).toBe('commands');
  expect(verbScope('commands', true, true)).toBeNull();
  expect(verbScope('commands', true, false)).toBe('all');
  expect(verbScope('commands', false, false)).toBeNull();
  expect(verbScope('all', false, false)).toBeNull();
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

test('phone Inbox tab: opens the notifications sheet, marks both triggers expanded, Esc gives the focus back', async () => {
  const {openOverlay, closeOverlay} = await import('./ds-shell.js');
  const saved = window.matchMedia;
  window.matchMedia = () => ({matches: false});
  document.body.innerHTML = `
    <a id="bell" href="/notifications" data-ds-open="ds-bell" aria-expanded="false">bell</a>
    <div id="ds-scrim"></div>
    <div class="ds-pop" id="ds-bell"><a href="/-/asc/inbox">Open inbox</a></div>
    <nav class="ds-bnav"><a id="tab" href="/notifications" data-ds-open="ds-bell" aria-expanded="false">Inbox</a></nav>`;
  const tab = document.querySelector('#tab');
  const sheet = document.querySelector('#ds-bell');
  let opened = 0;
  sheet.addEventListener('ds-open', () => opened++);
  tab.focus();
  try {
    expect(openOverlay('ds-bell', tab)).toBe(true);
    expect(sheet.classList.contains('is-open')).toBe(true);
    expect(opened).toBe(1);
    expect(tab.getAttribute('aria-expanded')).toBe('true');
    expect(document.querySelector('#bell').getAttribute('aria-expanded')).toBe('true');
    expect(document.body.classList.contains('ds-lock')).toBe(true);
    closeOverlay();
    expect(sheet.classList.contains('is-open')).toBe(false);
    expect(tab.getAttribute('aria-expanded')).toBe('false');
    expect(document.activeElement).toBe(tab);
  } finally {
    window.matchMedia = saved;
  }
});
