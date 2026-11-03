import {applyNameCheck, initOwnerVisibility} from './repo-new.js';

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

function ownerForm() {
  document.body.innerHTML = `<form>
    <select id="uid">
      <option value="1" data-default-visibility="public" selected>me</option>
      <option value="2" data-default-visibility="internal">acme</option>
      <option value="3" data-default-visibility="public">orbit</option>
    </select>
    <input type="radio" name="visibility" value="private">
    <input type="radio" name="visibility" value="internal">
    <input type="radio" name="visibility" value="public" checked>
  </form>`;
  const owner = document.querySelector('#uid');
  initOwnerVisibility(owner);
  const pick = (v) => {
    owner.value = v;
    owner.dispatchEvent(new Event('change'));
  };
  const checked = () => Array.from(document.querySelectorAll('input[name="visibility"]')).find((c) => c.checked)?.value;
  return {pick, checked};
}

test('the visibility follows the default of the owner chosen', () => {
  const {pick, checked} = ownerForm();
  pick('2');
  expect(checked()).toBe('internal');
  pick('3');
  expect(checked()).toBe('public');
});

test('a visibility the person picked stays when the owner changes', () => {
  const {pick, checked} = ownerForm();
  const priv = document.querySelector('input[value="private"]');
  priv.checked = true;
  priv.dispatchEvent(new Event('change'));
  pick('2');
  expect(checked()).toBe('private');
});
