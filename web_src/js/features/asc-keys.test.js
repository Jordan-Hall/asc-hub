import {isTypingTarget, nextRowIndex, initAscKeys, initAscInboxBadge} from './asc-keys.js';

test('nextRowIndex moves one row and never wraps', () => {
  expect(nextRowIndex(-1, 1, 3)).toBe(0);
  expect(nextRowIndex(-1, -1, 3)).toBe(2);
  expect(nextRowIndex(0, 1, 3)).toBe(1);
  expect(nextRowIndex(2, 1, 3)).toBe(2);
  expect(nextRowIndex(0, -1, 3)).toBe(0);
  expect(nextRowIndex(0, 1, 0)).toBe(-1);
});

test('isTypingTarget recognises fields', () => {
  document.body.innerHTML = '<input id="a"><textarea id="b"></textarea><select id="c"></select><a id="d" href="#">x</a>';
  expect(isTypingTarget(document.querySelector('#a'))).toBe(true);
  expect(isTypingTarget(document.querySelector('#b'))).toBe(true);
  expect(isTypingTarget(document.querySelector('#c'))).toBe(true);
  expect(isTypingTarget(document.querySelector('#d'))).toBe(false);
  expect(isTypingTarget(null)).toBe(false);
});

function press(key, init = {}) {
  document.dispatchEvent(new KeyboardEvent('keydown', {key, bubbles: true, ...init}));
}

test('j and k move the focus through the rows; typing and modifiers are ignored', () => {
  document.body.innerHTML = '<div data-asc-list><a href="#1" data-asc-row>1</a><a href="#2" data-asc-row>2</a><a href="#3" data-asc-row>3</a></div><input id="f">';
  initAscKeys();
  press('j');
  expect(document.activeElement.textContent).toBe('1');
  press('j');
  expect(document.activeElement.textContent).toBe('2');
  press('k');
  expect(document.activeElement.textContent).toBe('1');
  press('j', {ctrlKey: true});
  expect(document.activeElement.textContent).toBe('1');
  document.querySelector('#f').focus();
  document.querySelector('#f').dispatchEvent(new KeyboardEvent('keydown', {key: 'j', bubbles: true}));
  expect(document.activeElement.id).toBe('f');
});

test('s opens the snooze menu and p pins', () => {
  document.body.innerHTML = '<details data-asc-snooze><summary>s</summary><button type="button">1h</button></details><button data-asc-pin type="button">pin</button>';
  initAscKeys();
  const pin = document.querySelector('[data-asc-pin]');
  let pinned = 0;
  pin.addEventListener('click', () => pinned++);
  press('s');
  expect(document.querySelector('details').open).toBe(true);
  press('s');
  expect(document.querySelector('details').open).toBe(false);
  press('p');
  expect(pinned).toBe(1);
});

test('slash and Ctrl K focus the search box, but slash is a letter inside a field', () => {
  document.body.innerHTML = '<input data-asc-search id="q"><input id="other">';
  initAscKeys();
  press('/');
  expect(document.activeElement.id).toBe('q');
  document.querySelector('#other').focus();
  document.querySelector('#other').dispatchEvent(new KeyboardEvent('keydown', {key: '/', bubbles: true}));
  expect(document.activeElement.id).toBe('other');
  press('k', {ctrlKey: true});
  expect(document.activeElement.id).toBe('q');
});

test('the inbox badge shows the count and hides at zero', async () => {
  sessionStorage.clear();
  document.body.innerHTML = '<a href="/-/asc/inbox" data-asc-count-url="/-/asc/inbox/count">Inbox <span class="tw-hidden" data-asc-count></span></a>';
  globalThis.fetch = vi.fn().mockResolvedValue({ok: true, json: async () => ({count: 7})});
  await initAscInboxBadge();
  const badge = document.querySelector('[data-asc-count]');
  expect(badge.textContent).toBe('7');
  expect(badge.classList.contains('tw-hidden')).toBe(false);
  // the second page view in the same minute asks nobody
  document.body.innerHTML = '<a href="/-/asc/inbox" data-asc-count-url="/-/asc/inbox/count">Inbox <span class="tw-hidden" data-asc-count></span></a>';
  globalThis.fetch = vi.fn();
  await initAscInboxBadge();
  expect(globalThis.fetch).not.toHaveBeenCalled();
  expect(document.querySelector('[data-asc-count]').textContent).toBe('7');
  sessionStorage.clear();
  globalThis.fetch = vi.fn().mockResolvedValue({ok: true, json: async () => ({count: 0})});
  document.body.innerHTML = '<a href="/-/asc/inbox" data-asc-count-url="/-/asc/inbox/count">Inbox <span data-asc-count>5</span></a>';
  await initAscInboxBadge();
  expect(document.querySelector('[data-asc-count]').classList.contains('tw-hidden')).toBe(true);
  globalThis.fetch = vi.fn().mockResolvedValue({ok: false});
  sessionStorage.clear();
  await initAscInboxBadge();
});

test('on a diff j and k go from file to file and v marks the file as viewed', () => {
  document.body.innerHTML = `<div id="diff-file-boxes">
    <div class="diff-file-box" id="diff-a"><label class="viewed-file-form"><input type="checkbox"></label></div>
    <div class="diff-file-box" id="diff-b"><label class="viewed-file-form"><input type="checkbox"></label></div>
  </div>`;
  // the page is scrolled to the top: both files start below the top third of the window
  for (const el of document.querySelectorAll('.diff-file-box')) {
    el.scrollIntoView = () => {};
    el.getBoundingClientRect = () => ({top: 600});
  }
  initAscKeys();
  document.dispatchEvent(new KeyboardEvent('keydown', {key: 'j', bubbles: true}));
  expect(document.activeElement.id).toBe('diff-a');
  document.dispatchEvent(new KeyboardEvent('keydown', {key: 'j', bubbles: true}));
  expect(document.activeElement.id).toBe('diff-b');
  document.dispatchEvent(new KeyboardEvent('keydown', {key: 'v', bubbles: true}));
  expect(document.querySelector('#diff-b input').checked).toBe(true);
  expect(document.querySelector('#diff-a input').checked).toBe(false);
  document.dispatchEvent(new KeyboardEvent('keydown', {key: 'k', bubbles: true}));
  expect(document.activeElement.id).toBe('diff-a');
});
