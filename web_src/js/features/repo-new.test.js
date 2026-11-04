import {applyNameCheck, applyPreview, applyStart, busyText, importHref, initNewRepoPreview, initNewRepoStart, initOwnerVisibility, previewOf} from './repo-new.js';

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

function previewForm(policy) {
  document.body.innerHTML = `<form class="rp-new-form">
    <select id="uid"><option value="1" data-name="me">me</option><option value="2" data-name="acme" selected>acme</option></select>
    <input id="repo_name" value=" payments-v2 ">
    <label><input type="radio" name="visibility" value="private" checked><strong>Private</strong></label>
    <label><input type="radio" name="visibility" value="public"><strong>Public</strong></label>
    <input type="checkbox" name="policy_file"${policy ? ' checked' : ''}>
    <section data-new-preview>
      <strong data-pv-name data-noname="name"></strong><span data-pv-vis></span><span data-pv-policy hidden></span>
      <button data-pv-copy></button><span data-pv-url data-base="https://hub.example/"></span><span data-pv-dir></span>
    </section>
    <button data-new-submit data-busy-text="Creating {repo}…" data-busy-policy-text="Creating {repo} and its first policy…">Create</button>
  </form>`;
  return document.querySelector('form');
}

test('the preview follows the form', () => {
  const form = previewForm(true);
  const p = previewOf(form);
  expect(p).toEqual({owner: 'acme', name: 'payments-v2', visibility: 'Private', policy: true});
  const card = form.querySelector('[data-new-preview]');
  applyPreview(card, p);
  expect(card.querySelector('[data-pv-name]').textContent).toBe('acme / payments-v2');
  expect(card.querySelector('[data-pv-vis]').textContent).toBe('Private');
  expect(card.querySelector('[data-pv-policy]').hidden).toBe(false);
  expect(card.querySelector('[data-pv-url]').textContent).toBe('https://hub.example/acme/payments-v2.git');
  expect(card.querySelector('[data-pv-copy]').getAttribute('data-clipboard-text')).toBe('asc clone https://hub.example/acme/payments-v2.git\ncd payments-v2');

  applyPreview(card, {owner: 'acme', name: '', visibility: '', policy: false});
  expect(card.querySelector('[data-pv-name]').textContent).toBe('acme / name');
  expect(card.querySelector('[data-pv-vis]').textContent).toBe('Private');
  expect(card.querySelector('[data-pv-policy]').hidden).toBe(true);

  applyPreview(card, {owner: 'acme', name: '<img src=x>', visibility: '', policy: false});
  expect(card.querySelector('img')).toBeNull();
});

test('the button says what is being created', () => {
  const form = previewForm(true);
  const button = form.querySelector('[data-new-submit]');
  expect(busyText(button, previewOf(form))).toBe('Creating acme/payments-v2 and its first policy…');
  form.querySelector('input[name="policy_file"]').checked = false;
  expect(busyText(button, previewOf(form))).toBe('Creating acme/payments-v2…');
  button.removeAttribute('data-busy-policy-text');
  expect(busyText(button, {owner: 'acme', name: 'x', policy: true})).toBe('Creating acme/x…');
});

test('submit marks the button busy, coming back resets it', async () => {
  const form = previewForm(false);
  initNewRepoPreview(form);
  const button = form.querySelector('[data-new-submit]');
  form.dispatchEvent(new Event('submit'));
  expect(button.textContent).toBe('Creating acme/payments-v2…');
  expect(button.getAttribute('aria-busy')).toBe('true');
  await new Promise((r) => setTimeout(r, 0));
  expect(button.disabled).toBe(true);
  window.dispatchEvent(new Event('pageshow'));
  expect(button.textContent).toBe('Create');
  expect(button.disabled).toBe(false);
  expect(button.hasAttribute('aria-busy')).toBe(false);
});

function startForm() {
  document.body.innerHTML = `<form class="rp-new-form">
    <div data-new-start>
      <button type="button" data-start="empty" aria-pressed="true">Empty</button>
      <button type="button" data-start="template" aria-pressed="false">Template</button>
      <button type="button" data-start="import" aria-pressed="false">Import</button>
    </div>
    <select id="uid"><option value="7" selected>acme</option></select>
    <input id="repo_name" value=" ledger ">
    <textarea id="repo_desc">Books &amp; more</textarea>
    <label><input type="radio" name="visibility" value="private" checked></label>
    <section data-start-panel="import" hidden><a data-import-link href="/repo/migrate?service_type=2&org=1">GitHub</a></section>
    <section data-start-panel="template" hidden>
      <div id="repo_template_search" data-placeholder="Select template"><div class="text">acme/base</div></div>
      <input type="hidden" id="repo_template" value="12">
    </section>
    <div id="non_template" data-start-not-import></div>
    <div class="actions" data-start-not-import></div>
  </form>`;
  return document.querySelector('form');
}

test('importHref carries what was typed and replaces what the link had', () => {
  const form = startForm();
  expect(importHref('/repo/migrate?service_type=2&org=1', form)).toBe('/repo/migrate?service_type=2&org=7&repo_name=ledger&description=Books+%26+more&private=1');
  form.querySelector('#repo_name').value = '';
  form.querySelector('input[name="visibility"]').checked = false;
  expect(importHref('/repo/migrate?service_type=1&repo_name=old', form)).toBe('/repo/migrate?service_type=1&org=7&description=Books+%26+more');
});

test('Start from shows one way in at a time; Empty drops the template', () => {
  const form = startForm();
  const changed = vi.fn();
  form.querySelector('#repo_template').addEventListener('change', changed);
  initNewRepoStart(form);
  expect(form.querySelector('[data-import-link]').getAttribute('href')).toContain('repo_name=ledger');

  form.querySelector('[data-start="import"]').click();
  expect(form.querySelector('[data-start-panel="import"]').hidden).toBe(false);
  expect(form.querySelector('#non_template').hidden).toBe(true);
  expect(form.querySelector('.actions').hidden).toBe(true);
  expect(form.querySelector('[data-start="import"]').getAttribute('aria-pressed')).toBe('true');
  expect(form.querySelector('[data-start="empty"]').getAttribute('aria-pressed')).toBe('false');

  applyStart(form, 'template');
  expect(form.querySelector('[data-start-panel="template"]').hidden).toBe(false);
  expect(form.querySelector('[data-start-panel="import"]').hidden).toBe(true);
  expect(form.querySelector('.actions').hidden).toBe(false);
  expect(form.querySelector('#repo_template').value).toBe('12');

  form.querySelector('[data-start="empty"]').click();
  expect(form.querySelector('[data-start-panel="template"]').hidden).toBe(true);
  expect(form.querySelector('#repo_template').value).toBe('');
  expect(changed).toHaveBeenCalledTimes(1);
  const text = form.querySelector('#repo_template_search > .text');
  expect(text.textContent).toBe('Select template');
  expect(text.classList.contains('default')).toBe(true);

  form.querySelector('#repo_name').value = 'books';
  form.querySelector('#repo_name').dispatchEvent(new Event('input', {bubbles: true}));
  expect(form.querySelector('[data-import-link]').getAttribute('href')).toContain('repo_name=books');
});
