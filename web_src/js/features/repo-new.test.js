import {applyNameCheck} from './repo-new.js';

test('the name check says free or taken as text and marks the field', () => {
  document.body.innerHTML = '<input id="repo_name"><div id="s" data-ok-text="This name is free." hidden></div>';
  const line = document.querySelector('#s');
  applyNameCheck(line, {ok: true});
  expect(line.hidden).toBe(false);
  expect(line.textContent).toBe('This name is free.');
  expect(document.querySelector('#repo_name').getAttribute('aria-invalid')).toBe('false');

  applyNameCheck(line, {ok: false, message: '<b>acme/ledger</b> already exists.'});
  expect(line.textContent).toBe('<b>acme/ledger</b> already exists.');
  expect(line.querySelector('b')).toBeNull();
  expect(line.classList.contains('is-err')).toBe(true);
  expect(document.querySelector('#repo_name').getAttribute('aria-invalid')).toBe('true');

  applyNameCheck(line, null);
  expect(line.hidden).toBe(true);
});

test('the check can be pointed at another field, as the username field does', () => {
  document.body.innerHTML = '<input id="username"><div id="s" data-ok-text="Free." hidden></div>';
  const line = document.querySelector('#s');
  const input = document.querySelector('#username');
  applyNameCheck(line, {ok: false, message: 'Taken.'}, input);
  expect(line.textContent).toBe('Taken.');
  expect(input.getAttribute('aria-invalid')).toBe('true');
  applyNameCheck(line, {ok: true}, input);
  expect(input.getAttribute('aria-invalid')).toBe('false');
});
