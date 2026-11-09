import {isTypingTarget, nextRowIndex, initAscKeys, initAscInboxBadge, initAscInboxDone, doneFadeMs, nextChangeLink, initAscAgentsPill, agentsPillText} from './asc-keys.js';

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

test('e presses Mark done', () => {
  document.body.innerHTML = '<button data-asc-done type="button">done</button>';
  initAscKeys();
  let done = 0;
  document.querySelector('[data-asc-done]').addEventListener('click', () => done++);
  press('e');
  expect(done).toBe(1);
});

test('Mark done on a row fades the row, then sends the form once', () => {
  vi.useFakeTimers();
  document.body.innerHTML = '<ul><li class="wk-irow"><form data-asc-done-form><button type="submit">Mark done</button></form></li></ul>';
  const form = document.querySelector('form');
  const sent = vi.spyOn(form, 'submit').mockImplementation(() => {});
  initAscInboxDone();
  form.requestSubmit();
  expect(document.querySelector('.wk-irow').classList.contains('is-leaving')).toBe(true);
  expect(sent).not.toHaveBeenCalled();
  form.requestSubmit();
  vi.advanceTimersByTime(doneFadeMs);
  expect(sent).toHaveBeenCalledTimes(1);
  vi.useRealTimers();
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

const frame = () => new Promise((resolve) => requestAnimationFrame(() => resolve()));

function reviewPage({selfApprove = false} = {}) {
  document.body.innerHTML = `
    <div id="review-box"><button class="js-btn-review" type="button">Review</button></div>
    <div class="review-box-panel"><form>
      <textarea name="content"></textarea>
      <button type="submit" name="type" value="approve"${selfApprove ? ' disabled' : ''}>Approve</button>
      <button type="submit" name="type" value="comment">Comment</button>
      <button type="submit" name="type" value="reject">Request changes</button>
    </form></div>`;
  // the panel is in the page only while the popover shows it: it is "shown" after the button was clicked
  const opened = {count: 0};
  const panel = document.querySelector('.review-box-panel');
  panel.getClientRects = () => (opened.count > 0 ? [{}] : []);
  document.querySelector('.js-btn-review').addEventListener('click', () => opened.count++);
  return opened;
}

test('a, r and c open the review panel and put the focus on a control without sending anything', async () => {
  const opened = reviewPage();
  let sent = 0;
  document.querySelector('form').addEventListener('submit', (e) => {
    e.preventDefault();
    sent++;
  });
  initAscKeys();
  press('a');
  await frame();
  expect(document.activeElement.textContent).toBe('Approve');
  press('r');
  await frame();
  expect(document.activeElement.textContent).toBe('Request changes');
  press('c');
  await frame();
  expect(document.activeElement.getAttribute('name')).toBe('content');
  expect(opened.count).toBe(1); // the panel stays open for the next key
  expect(sent).toBe(0);
});

test('a leaves the focus on the comment field when the approve button is switched off', async () => {
  reviewPage({selfApprove: true});
  initAscKeys();
  press('a');
  await frame();
  expect(document.activeElement.getAttribute('name')).toBe('content');
});

test('a, r and c do nothing without a review box, or while typing, or with a modifier', async () => {
  document.body.innerHTML = '<input id="f"><button id="x">x</button>';
  initAscKeys();
  document.querySelector('#x').focus();
  press('a');
  press('c');
  await frame();
  expect(document.activeElement.id).toBe('x');

  const opened = reviewPage();
  initAscKeys();
  document.body.insertAdjacentHTML('beforeend', '<input id="g">');
  document.querySelector('#g').focus();
  document.querySelector('#g').dispatchEvent(new KeyboardEvent('keydown', {key: 'a', bubbles: true}));
  press('a', {ctrlKey: true});
  press('c', {metaKey: true});
  await frame();
  expect(opened.count).toBe(0);
});

test('on the inbox a and r go to Approve and Request changes of the open item without sending', async () => {
  document.body.innerHTML = `<form class="wk-reply" method="post" action="/o/r/pulls/1/files/reviews/submit">
    <textarea name="content"></textarea>
    <button type="submit" name="type" value="approve">Approve</button>
    <button type="submit" name="type" value="reject">Request changes</button>
    <button type="submit" name="type" value="comment">Reply</button></form>`;
  let sent = 0;
  document.querySelector('form').addEventListener('submit', (e) => {
    e.preventDefault();
    sent++;
  });
  initAscKeys();
  press('a');
  await frame();
  expect(document.activeElement.textContent).toBe('Approve');
  press('r');
  await frame();
  expect(document.activeElement.textContent).toBe('Request changes');
  expect(sent).toBe(0);
});

test('a review box that is switched off is left alone', async () => {
  const opened = reviewPage();
  document.querySelector('.js-btn-review').classList.add('disabled');
  initAscKeys();
  press('a');
  await frame();
  expect(opened.count).toBe(0);
});

test('n finds the change after the current one in the stack rail', () => {
  document.body.innerHTML = `<section data-asc-stack><ol>
    <li class="rv-stn"><a class="rv-stn-link" href="/p/1">1</a></li>
    <li class="rv-stn is-current"><a class="rv-stn-link" href="/p/2">2</a></li>
    <li class="rv-stn"><a class="rv-stn-link" href="/p/3">3</a></li></ol></section>`;
  expect(nextChangeLink().getAttribute('href')).toBe('/p/3');
  document.querySelector('.is-current').nextElementSibling.classList.add('is-current');
  document.querySelector('.is-current').classList.remove('is-current');
  expect(nextChangeLink()).toBeNull();
  document.body.innerHTML = '<p>no stack</p>';
  expect(nextChangeLink()).toBeNull();
});

const agentsPill = (testid) => `<a class="ds-agents-pill tw-hidden" href="/-/asc/agents" data-asc-agents-url="/-/asc/agents/count" data-word-one="{n} agent running" data-word-other="{n} agents running" data-testid="${testid}"><span class="ds-agents-dot" aria-hidden="true"></span><span data-asc-agents-n></span><span class="ds-agents-word" data-asc-agents-text></span></a>`;

test('the agents pill says how many runs are active, in the header and the top bar, and hides at zero', async () => {
  sessionStorage.clear();
  document.body.innerHTML = agentsPill('hdr-agents') + agentsPill('topbar-agents');
  globalThis.fetch = vi.fn().mockResolvedValue({ok: true, json: async () => ({count: 3})});
  await initAscAgentsPill();
  expect(globalThis.fetch).toHaveBeenCalledTimes(1);
  for (const pill of document.querySelectorAll('.ds-agents-pill')) {
    expect(pill.classList.contains('tw-hidden')).toBe(false);
    expect(pill.getAttribute('aria-label')).toBe('3 agents running');
    expect(pill.querySelector('[data-asc-agents-n]').textContent).toBe('3');
    expect(pill.querySelector('[data-asc-agents-text]').textContent).toBe('3 agents running');
  }
  // the next page within 30 seconds asks nobody
  document.body.innerHTML = agentsPill('hdr-agents');
  globalThis.fetch = vi.fn();
  await initAscAgentsPill();
  expect(globalThis.fetch).not.toHaveBeenCalled();
  expect(document.querySelector('.ds-agents-pill').classList.contains('tw-hidden')).toBe(false);

  sessionStorage.clear();
  globalThis.fetch = vi.fn().mockResolvedValue({ok: true, json: async () => ({count: 0})});
  await initAscAgentsPill();
  expect(document.querySelector('.ds-agents-pill').classList.contains('tw-hidden')).toBe(true);

  sessionStorage.clear();
  document.body.innerHTML = agentsPill('hdr-agents');
  globalThis.fetch = vi.fn().mockResolvedValue({ok: false});
  await initAscAgentsPill();
  expect(document.querySelector('.ds-agents-pill').classList.contains('tw-hidden')).toBe(true);
  globalThis.fetch = vi.fn().mockRejectedValue(new Error('offline'));
  await initAscAgentsPill();
  expect(document.querySelector('.ds-agents-pill').classList.contains('tw-hidden')).toBe(true);
});

test('the agents pill words are singular for one run', () => {
  document.body.innerHTML = agentsPill('hdr-agents');
  const pill = document.querySelector('.ds-agents-pill');
  expect(agentsPillText(pill, 1)).toBe('1 agent running');
  expect(agentsPillText(pill, 12)).toBe('12 agents running');
});
