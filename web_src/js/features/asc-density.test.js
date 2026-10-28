import {readDensity, applyDensity, initAscDensity} from './asc-density.js';

function page() {
  document.body.innerHTML = `<ol id="list" data-density="comfy"></ol>
    <div id="g" data-asc-density="#list"><button type="button" data-density="comfy" aria-pressed="true">a</button><button type="button" data-density="compact" aria-pressed="false">b</button></div>`;
}

beforeEach(() => {
  window.localStorage.clear();
});

test('readDensity falls back to comfy for nothing, junk or blocked storage', () => {
  expect(readDensity()).toBe('comfy');
  window.localStorage.setItem('asc-density', 'huge');
  expect(readDensity()).toBe('comfy');
  window.localStorage.setItem('asc-density', 'compact');
  expect(readDensity()).toBe('compact');
  expect(readDensity({getItem() { throw new Error('blocked'); }})).toBe('comfy');
});

test('applyDensity sets the list, the pressed button and the memory', () => {
  page();
  const group = document.querySelector('#g');
  expect(applyDensity(group, 'compact')).toBe(true);
  expect(document.querySelector('#list').getAttribute('data-density')).toBe('compact');
  const [comfy, compact] = group.querySelectorAll('button');
  expect(comfy.getAttribute('aria-pressed')).toBe('false');
  expect(compact.getAttribute('aria-pressed')).toBe('true');
  expect(window.localStorage.getItem('asc-density')).toBe('compact');
  expect(applyDensity(group, 'tiny')).toBe(false);
});

test('a click on a button switches the density and a remembered choice is applied at load', () => {
  window.localStorage.setItem('asc-density', 'compact');
  page();
  initAscDensity();
  expect(document.querySelector('#list').getAttribute('data-density')).toBe('compact');
  document.querySelector('button[data-density="comfy"]').click();
  expect(document.querySelector('#list').getAttribute('data-density')).toBe('comfy');
});
