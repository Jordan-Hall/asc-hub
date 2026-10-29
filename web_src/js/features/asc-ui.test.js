import {archFromHints, osFromPlatform, initAscInstallPills, initDocsFilter, initStackFilter, initRefFilter, initDocTabs} from './asc-ui.js';

test('archFromHints names only the builds asc ships', () => {
  expect(archFromHints({architecture: 'arm', bitness: '64'})).toBe('arm64');
  expect(archFromHints({architecture: 'x86', bitness: '64'})).toBe('x86_64');
  expect(archFromHints({architecture: 'riscv'})).toBeNull();
  expect(archFromHints({})).toBeNull();
  expect(archFromHints(null)).toBeNull();
  expect(archFromHints({architecture: 5})).toBeNull();
});

test('osFromPlatform maps the client hint', () => {
  expect(osFromPlatform('Windows')).toBe('windows');
  expect(osFromPlatform('macOS')).toBe('macos');
  expect(osFromPlatform('Linux')).toBe('linux');
  expect(osFromPlatform('Android')).toBeNull();
  expect(osFromPlatform(undefined)).toBeNull();
});

test('install pills press one pill and show only its command', () => {
  document.body.innerHTML = `<div data-ds-install>
    <button class="ds-install-pill" data-install-key="brew" aria-pressed="true"></button>
    <button class="ds-install-pill" data-install-key="scoop" aria-pressed="false"></button>
    <div class="ds-cmd" data-install-key="brew"></div><div class="ds-cmd" data-install-key="scoop" hidden></div></div>`;
  initAscInstallPills();
  document.querySelector('[data-install-key="scoop"].ds-install-pill').click();
  expect(Array.from(document.querySelectorAll('.ds-install-pill'), (p) => p.getAttribute('aria-pressed'))).toEqual(['false', 'true']);
  expect(Array.from(document.querySelectorAll('.ds-cmd'), (c) => c.hidden)).toEqual([true, false]);
});

test('docs filter hides the guide cards that do not match', () => {
  document.body.innerHTML = `<input data-docs-filter><p data-docs-none hidden></p><ul class="site-doccards"><li>Stacks and operations undo</li><li>Storage and keys</li></ul>`;
  initDocsFilter();
  const input = document.querySelector('input');
  input.value = 'keys';
  input.dispatchEvent(new Event('input'));
  expect(Array.from(document.querySelectorAll('li'), (li) => li.hidden)).toEqual([true, false]);
  expect(document.querySelector('[data-docs-none]').hidden).toBe(true);
  input.value = 'zzz';
  input.dispatchEvent(new Event('input'));
  expect(document.querySelector('[data-docs-none]').hidden).toBe(false);
});

test('stack filter shows the rows of the pressed button and a note when none match', () => {
  document.body.innerHTML = `<section><div data-stack-filter>
    <button type="button" data-stack-show="all" aria-pressed="true"></button>
    <button type="button" data-stack-show="mine" aria-pressed="false"></button>
    <button type="button" data-stack-show="agent" aria-pressed="false"></button></div>
    <ul><li data-stack-row data-mine="true" data-agent="false"></li><li data-stack-row data-mine="false" data-agent="true"></li></ul>
    <p data-stack-none hidden></p></section>`;
  initStackFilter();
  const [all, mine, agent] = document.querySelectorAll('button');
  const hidden = () => Array.from(document.querySelectorAll('li'), (li) => li.hidden);
  mine.click();
  expect(hidden()).toEqual([false, true]);
  expect([all, mine, agent].map((b) => b.getAttribute('aria-pressed'))).toEqual(['false', 'true', 'false']);
  agent.click();
  expect(hidden()).toEqual([true, false]);
  all.click();
  expect(hidden()).toEqual([false, false]);
  document.querySelectorAll('li')[0].setAttribute('data-agent', 'false');
  document.querySelectorAll('li')[1].setAttribute('data-agent', 'false');
  agent.click();
  expect(document.querySelector('[data-stack-none]').hidden).toBe(false);
});

test('ref pills show the sections of one kind and All shows every section', () => {
  document.body.innerHTML = `<div data-ref-filter>
    <button type="button" data-ref-show="all" aria-pressed="true"></button>
    <button type="button" data-ref-show="stacks" aria-pressed="false"></button>
    <button type="button" data-ref-show="tags" aria-pressed="false"></button></div>
    <section data-ref-kind="shared"></section><section data-ref-kind="stacks"></section><section data-ref-kind="tags"></section>`;
  initRefFilter();
  const [all, stacks] = document.querySelectorAll('button');
  const hidden = () => Array.from(document.querySelectorAll('section'), (s) => s.hidden);
  stacks.click();
  expect(hidden()).toEqual([true, false, true]);
  expect(stacks.getAttribute('aria-pressed')).toBe('true');
  expect(all.getAttribute('aria-pressed')).toBe('false');
  all.click();
  expect(hidden()).toEqual([false, false, false]);
});

test('doc tabs show the panel of the pressed tab and move with the arrow keys', () => {
  document.body.innerHTML = `<div data-doc-tabs>
    <button type="button" data-doc-tab="readme" aria-selected="true"></button>
    <button type="button" data-doc-tab="agents" aria-selected="false"></button></div>
    <section data-doc-panel="readme"></section><section data-doc-panel="agents" hidden></section>`;
  initDocTabs();
  const [readme, agents] = document.querySelectorAll('button');
  const hidden = () => Array.from(document.querySelectorAll('section'), (s) => s.hidden);
  agents.click();
  expect(hidden()).toEqual([true, false]);
  expect(agents.getAttribute('aria-selected')).toBe('true');
  expect(readme.getAttribute('aria-selected')).toBe('false');
  agents.dispatchEvent(new KeyboardEvent('keydown', {key: 'ArrowRight'}));
  expect(hidden()).toEqual([false, true]);
});
