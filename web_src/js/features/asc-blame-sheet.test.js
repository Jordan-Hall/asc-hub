import {HOLD_MS, initAscBlameSheet, lineTitle, openChange, openLines, selectedLines} from './asc-blame-sheet.js';

const sheet = `<dialog data-blame-sheet data-t-line="Line {a}" data-t-lines="Lines {a} to {b}">
  <h2 data-blame-sheet-h></h2>
  <div data-blame-sheet-change hidden></div>
  <div data-blame-sheet-line hidden>
    <button data-blame-share hidden>Share</button>
    <button data-blame-copy>Copy</button>
    <a data-blame-issue href="/o/r/issues/new">Issue</a>
  </div>
</dialog>`;

const page = `<div class="code-view"><table>
  <tr><td class="lines-commit"><div class="blame-info">
    <button type="button" class="blame-avatar rp-blame-open" data-blame-open>a</button>
    <div class="rp-blame-card" role="tooltip">
      <strong class="rp-blame-card-t">Parse the header</strong>
      <span class="rp-blame-card-m">Ada <span class="rp-hash">abc1234</span></span>
      <span class="rp-blame-card-a"><a class="ds-btn" href="/o/r/commit/abc">Open</a></span>
    </div>
  </div></td><td class="lines-num"><span id="L1" data-line-number="1"></span></td><td rel="L1" class="lines-code blame-code"></td></tr>
  <tr><td class="lines-commit"></td><td class="lines-num"><span id="L2" data-line-number="2"></span></td><td rel="L2" class="lines-code blame-code"></td></tr>
</table></div>
<div class="code-line-menu">
  <a class="ref-in-new-issue" href="/o/r/issues/new?body=x%23L1-L2"></a>
  <a class="copy-line-permalink" data-url="/o/r/src/commit/abc/a.go#L1-L2"></a>
</div>${sheet}`;

function mount() {
  document.body.innerHTML = page;
  const dialog = document.querySelector('dialog');
  dialog.showModal = function () {
    this.open = true;
  };
  dialog.close = function () {
    this.open = false;
  };
  return dialog;
}

test('the heading names one line or a range', () => {
  const dialog = mount();
  expect(lineTitle(dialog, 4, 4)).toBe('Line 4');
  expect(lineTitle(dialog, 4, 9)).toBe('Lines 4 to 9');
});

test('the selection comes from the hash repo-code writes', () => {
  expect(selectedLines('#L3')).toEqual([3, 3]);
  expect(selectedLines('#L3-L7')).toEqual([3, 7]);
  expect(selectedLines('#n3')).toBe(null);
  expect(selectedLines('')).toBe(null);
  expect(selectedLines('#L3-L7x')).toBe(null);
});

test('an avatar opens its change: the message as heading, the author and actions as copies', () => {
  const dialog = mount();
  expect(openChange(dialog, document.querySelector('[data-blame-open]'))).toBe(true);
  expect(dialog.open).toBe(true);
  expect(dialog.querySelector('[data-blame-sheet-h]').textContent).toBe('Parse the header');
  const body = dialog.querySelector('[data-blame-sheet-change]');
  expect(body.hidden).toBe(false);
  expect(dialog.querySelector('[data-blame-sheet-line]').hidden).toBe(true);
  expect(body.querySelector('.rp-blame-card-t')).toBe(null);
  expect(body.querySelector('.rp-blame-card-m').textContent).toContain('Ada');
  expect(body.querySelector('a').getAttribute('href')).toBe('/o/r/commit/abc');
  expect(document.querySelector('.rp-blame-card .rp-blame-card-m')).not.toBe(null);
});

test('a row without a card opens nothing', () => {
  const dialog = mount();
  const lone = document.createElement('button');
  document.body.append(lone);
  expect(openChange(dialog, lone)).toBe(false);
  expect(dialog.open).toBeFalsy();
});

test('the line sheet takes the selection and the issue link', () => {
  const dialog = mount();
  window.location.hash = '#L1-L2';
  expect(openLines(dialog)).toBe(true);
  expect(dialog.querySelector('[data-blame-sheet-h]').textContent).toBe('Lines 1 to 2');
  expect(dialog.querySelector('[data-blame-issue]').getAttribute('href')).toBe('/o/r/issues/new?body=x%23L1-L2');
  expect(dialog.querySelector('[data-blame-sheet-line]').hidden).toBe(false);
  expect(dialog.querySelector('[data-blame-share]').hidden).toBe(typeof navigator.share !== 'function');
  window.location.hash = '';
  expect(openLines(dialog)).toBe(false);
});

function press(target, type, opts = {}) {
  const e = new MouseEvent(type, {bubbles: true, clientX: opts.x ?? 0, clientY: opts.y ?? 0});
  Object.defineProperty(e, 'pointerType', {value: opts.pointerType ?? 'touch'});
  target.dispatchEvent(e);
}

test('a long press on a line number selects it and opens the line sheet; a short one or a drag does not', () => {
  vi.useFakeTimers();
  window.matchMedia = () => ({matches: true});
  const dialog = mount();
  initAscBlameSheet();
  const span = document.querySelector('#L2');
  const clicks = [];
  document.addEventListener('click', (e) => {
    if (e.target === span) {
      clicks.push(e.shiftKey);
      window.location.hash = '#L2';
    }
  });

  press(span, 'pointerdown');
  vi.advanceTimersByTime(HOLD_MS - 50);
  press(span, 'pointerup');
  vi.advanceTimersByTime(HOLD_MS);
  expect(clicks).toEqual([]);

  press(span, 'pointerdown');
  press(span, 'pointermove', {x: 30, y: 0});
  vi.advanceTimersByTime(HOLD_MS);
  expect(clicks).toEqual([]);

  press(span, 'pointerdown', {pointerType: 'mouse'});
  vi.advanceTimersByTime(HOLD_MS);
  expect(clicks).toEqual([]);

  press(span, 'pointerdown');
  vi.advanceTimersByTime(HOLD_MS);
  expect(clicks).toEqual([false]);
  expect(dialog.open).toBe(true);
  expect(dialog.querySelector('[data-blame-sheet-h]').textContent).toBe('Line 2');
  vi.useRealTimers();
  window.location.hash = '';
});

test('a long press with another line selected extends the selection', () => {
  vi.useFakeTimers();
  window.matchMedia = () => ({matches: true});
  mount();
  initAscBlameSheet();
  document.querySelector('[rel="L1"]').closest('tr').classList.add('active');
  const span = document.querySelector('#L2');
  let shift = null;
  span.addEventListener('click', (e) => {
    shift = e.shiftKey;
  });
  press(span, 'pointerdown');
  vi.advanceTimersByTime(HOLD_MS);
  expect(shift).toBe(true);
  vi.useRealTimers();
});

test('on a wide screen the avatar leaves the hover card alone', () => {
  window.matchMedia = () => ({matches: false});
  const dialog = mount();
  initAscBlameSheet();
  document.querySelector('[data-blame-open]').click();
  expect(dialog.open).toBeFalsy();
  window.matchMedia = () => ({matches: true});
  const narrow = mount();
  initAscBlameSheet();
  document.querySelector('[data-blame-open]').click();
  expect(narrow.open).toBe(true);
});
