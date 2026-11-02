import {watchDiffAutoLoad} from './asc-diff-autoload.js';

class FakeObserver {
  static last;
  constructor(cb, opts) {
    this.cb = cb;
    this.opts = opts;
    this.watched = new Set();
    FakeObserver.last = this;
  }
  observe(el) { this.watched.add(el) }
  unobserve(el) { this.watched.delete(el) }
  disconnect() { this.watched.clear() }
  fire(el, isIntersecting = true) { this.cb([{target: el, isIntersecting}]) }
}

function page() {
  document.body.innerHTML = `<div id="diff-file-boxes">
    <div><a class="diff-load-button" data-href="?files=a">Load diff</a></div>
    <div id="diff-incomplete"><a id="diff-show-more-files" data-href="?skip-to=x">Show more</a></div>
  </div>`;
  return document.querySelector('#diff-file-boxes');
}

test('the next page and large files load as they near the viewport, once each', () => {
  const root = page();
  const more = [], files = [];
  watchDiffAutoLoad(root, {onMore: (u) => more.push(u), onFile: (b) => files.push(b)}, FakeObserver);
  const io = FakeObserver.last;
  expect(io.opts.rootMargin).toBe('2000px 0px');
  expect(io.watched.size).toBe(2);

  const btn = root.querySelector('.diff-load-button');
  io.fire(btn, false);
  expect(files).toEqual([]);
  io.fire(btn);
  expect(files).toEqual([btn]);
  expect(io.watched.has(btn)).toBe(false);

  io.fire(root.querySelector('#diff-show-more-files'));
  expect(more).toEqual(['?skip-to=x']);
});

test('buttons added by a later page are watched too', async () => {
  const root = page();
  watchDiffAutoLoad(root, {onMore() {}, onFile() {}}, FakeObserver);
  const io = FakeObserver.last;
  root.insertAdjacentHTML('beforeend', '<div><a class="diff-load-button" data-href="?files=b">Load</a></div>');
  await new Promise((r) => setTimeout(r, 0));
  expect(io.watched.size).toBe(3);
});

test('without IntersectionObserver the buttons stay manual', () => {
  const stop = watchDiffAutoLoad(page(), {onMore() {}, onFile() {}}, undefined);
  expect(typeof stop).toBe('function');
});
