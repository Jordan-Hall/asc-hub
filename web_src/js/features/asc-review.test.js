import {
  applyChangedOnly, applyClassFilter, fillCount, initChangedOnly, initClassFilter, initDiffState, initRailScroll,
  initReviewCard, openThreadCount, railKey, saveRailScroll, setDiffLoading, showDiffError, takeRailScroll, viewedCounts,
} from './asc-review.js';

function memoryStorage() {
  const map = new Map();
  return {
    getItem: (k) => (map.has(k) ? map.get(k) : null),
    setItem: (k, v) => map.set(k, v),
    removeItem: (k) => map.delete(k),
    map,
  };
}

function files(...classes) {
  return classes.map((c, i) => `<div class="rv-file" id="diff-${i}" data-asc-class="${c}"><span class="${c === 'Logic' && i === 0 ? 'changed-since-last-review' : ''}"></span></div>`).join('');
}

test('the rail position of a change is saved under its path and taken once', () => {
  const storage = memoryStorage();
  saveRailScroll(storage, '/o/r/pulls/3', 412.6);
  expect(storage.getItem(railKey('/o/r/pulls/3'))).toBe('413');
  expect(takeRailScroll(storage, '/o/r/pulls/4')).toBeNull();
  expect(takeRailScroll(storage, '/o/r/pulls/3')).toBe(413);
  expect(takeRailScroll(storage, '/o/r/pulls/3')).toBeNull();
});

test('a bad saved position is dropped and a blocked storage does not throw', () => {
  const storage = memoryStorage();
  storage.setItem(railKey('/p'), 'abc');
  expect(takeRailScroll(storage, '/p')).toBeNull();
  const broken = {setItem() { throw new Error('full') }, getItem() { throw new Error('blocked') }, removeItem() {}};
  expect(() => saveRailScroll(broken, '/p', 5)).not.toThrow();
  expect(takeRailScroll(broken, '/p')).toBeNull();
});

test('leaving a change by the rail saves the scroll, coming back restores it', () => {
  const storage = memoryStorage();
  document.body.innerHTML = '<section data-asc-stack><ol><li class="is-current"><a class="rv-stn-link" href="/o/r/pulls/3">3</a></li><li><a class="rv-stn-link" href="/o/r/pulls/4">4</a></li></ol></section>';
  const win = {sessionStorage: storage, scrollY: 640, location: {pathname: '/o/r/pulls/3', hash: ''}, scrollTo: vi.fn()};
  initRailScroll(document, win);
  expect(win.scrollTo).not.toHaveBeenCalled();
  document.querySelector('.is-current a').dispatchEvent(new window.MouseEvent('click', {bubbles: true}));
  expect(storage.map.size).toBe(0);
  document.querySelectorAll('.rv-stn-link')[1].dispatchEvent(new window.MouseEvent('click', {bubbles: true}));
  expect(storage.getItem(railKey('/o/r/pulls/3'))).toBe('640');

  const back = {sessionStorage: storage, scrollY: 0, location: {pathname: '/o/r/pulls/3', hash: ''}, scrollTo: vi.fn()};
  initRailScroll(document, back);
  expect(back.scrollTo).toHaveBeenCalledWith(0, 640);
});

test('a link with a hash in the address keeps the browser position', () => {
  const storage = memoryStorage();
  saveRailScroll(storage, '/o/r/pulls/3', 90);
  document.body.innerHTML = '<section data-asc-stack></section>';
  const win = {sessionStorage: storage, scrollY: 0, location: {pathname: '/o/r/pulls/3', hash: '#diff-x'}, scrollTo: vi.fn()};
  initRailScroll(document, win);
  expect(win.scrollTo).not.toHaveBeenCalled();
});

test('fillCount fills the numbers in order', () => {
  expect(fillCount('Showing %d of %d files', 2, 5)).toBe('Showing 2 of 5 files');
  expect(fillCount('%d open comments', 0)).toBe('0 open comments');
});

test('the class filter shows the files of one class and all of them again', () => {
  document.body.innerHTML = files('Logic', 'Docs', 'Logic', '');
  expect(applyClassFilter(document, 'Logic')).toEqual({shown: 2, total: 4});
  expect(Array.from(document.querySelectorAll('.rv-file'), (f) => f.hidden)).toEqual([false, true, false, true]);
  expect(applyClassFilter(document, '')).toEqual({shown: 4, total: 4});
  expect(document.querySelectorAll('.rv-file[hidden]').length).toBe(0);
});

test('the class chips set aria-pressed, say how many files show and clicking the chip again clears it', () => {
  document.body.innerHTML = `<div data-asc-classbar data-tpl-one="Showing %d of %d file" data-tpl-other="Showing %d of %d files">
    <button data-asc-class="" aria-pressed="true">All</button><button data-asc-class="Logic">Logic</button><button data-asc-class="Docs">Docs</button>
    <span data-asc-class-status></span></div><div id="diff-file-boxes">${files('Logic', 'Docs', 'Logic')}</div>`;
  initClassFilter(document);
  const chips = document.querySelectorAll('button[data-asc-class]');
  chips[2].click();
  expect(chips[2].getAttribute('aria-pressed')).toBe('true');
  expect(chips[0].getAttribute('aria-pressed')).toBe('false');
  expect(document.querySelector('[data-asc-class-status]').textContent).toBe('Showing 1 of 3 file');
  chips[1].click();
  expect(document.querySelector('[data-asc-class-status]').textContent).toBe('Showing 2 of 3 files');
  chips[1].click();
  expect(chips[1].getAttribute('aria-pressed')).toBe('false');
  expect(document.querySelector('[data-asc-class-status]').textContent).toBe('');
  expect(document.querySelectorAll('.rv-file[hidden]').length).toBe(0);
});

test('files that load later get the filter that is on', async () => {
  document.body.innerHTML = `<div data-asc-classbar><button data-asc-class="Docs">Docs</button><span data-asc-class-status></span></div><div id="diff-file-boxes">${files('Docs')}</div>`;
  initClassFilter(document);
  document.querySelector('button[data-asc-class]').click();
  document.getElementById('diff-file-boxes').insertAdjacentHTML('beforeend', '<div class="rv-file" id="late" data-asc-class="Logic"></div>');
  await new Promise((resolve) => setTimeout(resolve, 0));
  expect(document.getElementById('late').hidden).toBe(true);
});

test('only the files changed since the last review stay when that filter is on, and the class filter still holds', () => {
  document.body.innerHTML = files('Logic', 'Logic', 'Docs');
  expect(applyChangedOnly(document, true)).toEqual({shown: 1, total: 3});
  expect(Array.from(document.querySelectorAll('.rv-file'), (f) => f.hidden)).toEqual([false, true, true]);
  applyClassFilter(document, 'Docs');
  expect(document.querySelector('#diff-0').hidden).toBe(true);
  applyChangedOnly(document, false);
  expect(Array.from(document.querySelectorAll('.rv-file'), (f) => f.hidden)).toEqual([true, true, false]);
});

test('the changed-only button toggles the filter and its own words', () => {
  document.body.innerHTML = `<button data-asc-changed-only aria-pressed="false" data-on="Show every file" data-off="Show only what changed">Show only what changed</button>${files('Logic', 'Docs')}`;
  initChangedOnly(document);
  const button = document.querySelector('[data-asc-changed-only]');
  button.click();
  expect(button.getAttribute('aria-pressed')).toBe('true');
  expect(button.textContent).toBe('Show every file');
  expect(document.querySelector('#diff-1').hidden).toBe(true);
  button.click();
  expect(button.textContent).toBe('Show only what changed');
  expect(document.querySelector('#diff-1').hidden).toBe(false);
});

test('open threads are the conversations that are not resolved', () => {
  document.body.innerHTML = `
    <div class="conversation-holder" data-path="a"><div class="comment-list"></div></div>
    <div class="conversation-holder" data-path="b"><div class="resolved-placeholder"></div><div class="comment-list"></div></div>
    <div class="conversation-holder"><div class="comment-list"></div></div>
    <div class="conversation-holder" data-path="c"><div class="comment-list"></div></div>`;
  expect(openThreadCount(document)).toBe(2);
});

test('viewed counts come from the progress bar, else from the page', () => {
  document.body.innerHTML = '<progress id="viewed-files-summary" value="2" max="5"></progress>';
  expect(viewedCounts(document, 0, 9)).toEqual({viewed: 2, total: 5});
  document.body.innerHTML = '';
  expect(viewedCounts(document, 1, 3)).toEqual({viewed: 1, total: 3});
});

test('the review card shows what was opened and what is still open, and remembers the intent box', async () => {
  window.localStorage.clear();
  document.body.innerHTML = `
    <section data-asc-review-card data-pull="/o/r/pulls/3" data-viewed="1" data-total="3"
      data-tpl-files-one="Opened %d of %d file" data-tpl-files-other="Opened %d of %d files"
      data-tpl-threads-one="%d open comment" data-tpl-threads-other="%d open comments">
      <ul><li><input type="checkbox" data-asc-you-intent></li><li><span data-asc-you-files></span></li><li><span data-asc-you-threads></span></li></ul>
      <button data-asc-open-review="comment">Comment</button>
    </section>
    <button class="js-btn-review"></button><div class="review-box-panel"><textarea></textarea></div>
    <div id="diff-container"><div class="conversation-holder" data-path="a"><div class="comment-list"></div></div></div>`;
  const opened = vi.fn();
  document.querySelector('.js-btn-review').addEventListener('click', opened);
  initReviewCard(document);
  expect(document.querySelector('[data-asc-you-files]').textContent).toBe('Opened 1 of 3 files');
  expect(document.querySelector('[data-asc-you-threads]').textContent).toBe('1 open comment');
  expect(document.querySelector('[data-asc-you-threads]').closest('li').classList.contains('is-done')).toBe(false);

  document.querySelector('.conversation-holder').insertAdjacentHTML('afterbegin', '<div class="resolved-placeholder"></div>');
  await new Promise((resolve) => setTimeout(resolve, 150));
  expect(document.querySelector('[data-asc-you-threads]').textContent).toBe('0 open comments');
  expect(document.querySelector('[data-asc-you-threads]').closest('li').classList.contains('is-done')).toBe(true);

  const box = document.querySelector('[data-asc-you-intent]');
  box.checked = true;
  box.dispatchEvent(new window.Event('change', {bubbles: true}));
  expect(window.localStorage.getItem('asc-you-intent:/o/r/pulls/3')).toBe('1');

  document.querySelector('[data-asc-open-review]').click();
  expect(opened).toHaveBeenCalledTimes(1);
});

test('the skeleton shows while files load and the error offers Try again once', () => {
  document.body.innerHTML = `<div data-asc-diff-state>
    <div data-asc-skeleton hidden></div>
    <div data-asc-diff-error hidden><button data-asc-diff-retry>Try again</button></div></div>`;
  initDiffState(document);
  expect(setDiffLoading(true)).toBe(true);
  expect(document.querySelector('[data-asc-skeleton]').hidden).toBe(false);
  setDiffLoading(false);
  expect(document.querySelector('[data-asc-skeleton]').hidden).toBe(true);

  const retry = vi.fn();
  expect(showDiffError(retry)).toBe(true);
  expect(document.querySelector('[data-asc-diff-error]').hidden).toBe(false);
  document.querySelector('[data-asc-diff-retry]').click();
  expect(retry).toHaveBeenCalledTimes(1);
  expect(document.querySelector('[data-asc-diff-error]').hidden).toBe(true);
  document.querySelector('[data-asc-diff-retry]').click();
  expect(retry).toHaveBeenCalledTimes(1);

  setDiffLoading(true);
  showDiffError(() => {});
  setDiffLoading(true);
  expect(document.querySelector('[data-asc-diff-error]').hidden).toBe(true);
});

test('a page without the skeleton or the error gets false, so the caller can use a toast', () => {
  document.body.innerHTML = '';
  expect(setDiffLoading(true)).toBe(false);
  expect(showDiffError(() => {})).toBe(false);
});
