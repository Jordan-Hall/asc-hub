import {initAscRowMenus} from './asc-row-menu.js';

const row = (n) => `<li>
  <a id="name${n}" href="/acme/r${n}">r${n}</a>
  <details class="wk-menu ds-row-menu" data-asc-row-menu id="menu${n}">
    <summary aria-label="Actions for acme/r${n}">…</summary>
    <div class="wk-menu-list" role="group"><button type="button" class="wk-menu-item" id="item${n}">Copy clone command</button></div>
  </details>
</li>`;

const click = (el) => el.dispatchEvent(new MouseEvent('click', {bubbles: true, cancelable: true, button: 0}));

test('one row menu is open at a time; a tap outside or an item closes it', () => {
  document.body.innerHTML = `<ul>${row(1)}${row(2)}</ul><p id="out">x</p>`;
  initAscRowMenus();
  const one = document.querySelector('#menu1');
  const two = document.querySelector('#menu2');
  one.open = true;
  one.dispatchEvent(new Event('toggle'));
  two.open = true;
  two.dispatchEvent(new Event('toggle'));
  expect(one.open).toBe(false);
  expect(two.open).toBe(true);

  click(document.querySelector('#menu2 .wk-menu-list'));
  expect(two.open).toBe(true);
  click(document.querySelector('#out'));
  expect(two.open).toBe(false);

  one.open = true;
  click(document.querySelector('#item1'));
  expect(one.open).toBe(false);
});

test('Escape closes the menu and gives the focus back to its button', () => {
  document.body.innerHTML = `<ul>${row(1)}</ul>`;
  initAscRowMenus();
  const menu = document.querySelector('#menu1');
  menu.open = true;
  const item = document.querySelector('#item1');
  item.focus();
  item.dispatchEvent(new KeyboardEvent('keydown', {key: 'Escape', bubbles: true}));
  expect(menu.open).toBe(false);
  expect(document.activeElement).toBe(menu.querySelector('summary'));
});

test('a page without row menus gets no listeners', () => {
  document.body.innerHTML = '<p>nothing</p>';
  const spy = vi.spyOn(document, 'addEventListener');
  initAscRowMenus();
  expect(spy).not.toHaveBeenCalled();
  spy.mockRestore();
});
