import {initOrgNew} from './org-new.js';

function page(name = '') {
  document.body.innerHTML = `<form id="org-new-form" data-check-url="/org/create/check" data-checking="Checking…">
    <input id="org_name" name="org_name" value="${name}"><input id="full_name" name="full_name">
    <p id="org_name_status"></p>
    <input type="radio" id="vis-public" name="visibility" value="0"><label for="vis-public">Public</label>
    <input type="radio" id="vis-private" name="visibility" value="2" checked><label for="vis-private">Private</label>
    <span data-preview-initial>?</span><p data-preview-name data-empty="your-org"></p>
    <p data-preview-url data-base="https://hub.example.org/"></p><span data-preview-visibility></span>
    <button data-testid="org-create-submit">Create</button>
  </form>`;
  return document.querySelector('#org-new-form');
}

const text = (sel) => document.querySelector(sel).textContent;

function typeInto(input, value) {
  input.value = value;
  input.dispatchEvent(new Event('input', {bubbles: true}));
}

test('the preview follows the handle and the visibility', () => {
  const form = page();
  initOrgNew(form, {ask: async () => ({ok: true, message: ''}), schedule: () => 0, cancel: () => {}});
  expect(text('[data-preview-name]')).toBe('your-org');
  expect(text('[data-preview-initial]')).toBe('?');
  expect(text('[data-preview-visibility]')).toBe('Private');
  typeInto(form.querySelector('#org_name'), 'nova');
  expect(text('[data-preview-name]')).toBe('nova');
  expect(text('[data-preview-initial]')).toBe('N');
  expect(text('[data-preview-url]')).toBe('https://hub.example.org/nova');
  const pub = form.querySelector('#vis-public');
  pub.checked = true;
  pub.dispatchEvent(new Event('change', {bubbles: true}));
  expect(text('[data-preview-visibility]')).toBe('Public');
  typeInto(form.querySelector('#full_name'), 'Orbit Labs');
  expect(text('[data-preview-name]')).toBe('Orbit Labs');
  expect(text('[data-preview-initial]')).toBe('O');
  expect(text('[data-preview-url]')).toBe('https://hub.example.org/nova');
});

test('the handle is checked after a pause and only the latest answer is shown', async () => {
  const form = page();
  const jobs = [];
  const answers = [];
  const ask = (url, name) => new Promise((resolve) => answers.push({url, name, resolve}));
  initOrgNew(form, {ask, schedule: (fn) => jobs.push(fn), cancel: () => {}});
  const input = form.querySelector('#org_name');
  typeInto(input, 'nov');
  jobs.shift()();
  typeInto(input, 'nova');
  jobs.shift()();
  expect(answers.map((a) => [a.url, a.name])).toEqual([['/org/create/check', 'nov'], ['/org/create/check', 'nova']]);
  expect(text('#org_name_status')).toBe('Checking…');
  answers[1].resolve({ok: false, message: 'The organization name is already taken.'});
  await Promise.resolve();
  answers[0].resolve({ok: true, message: 'nov is free.'});
  await Promise.resolve();
  await Promise.resolve();
  expect(text('#org_name_status')).toBe('The organization name is already taken.');
  expect(input.getAttribute('aria-invalid')).toBe('true');
  expect(document.querySelector('#org_name_status').classList.contains('ds-c-err')).toBe(true);
});

test('a free handle is shown in green and clears the error', async () => {
  const form = page('nova');
  const input = form.querySelector('#org_name');
  input.setAttribute('aria-invalid', 'true');
  initOrgNew(form, {ask: async () => ({ok: true, message: 'nova is free.'}), schedule: () => 0, cancel: () => {}});
  await Promise.resolve();
  await Promise.resolve();
  expect(text('#org_name_status')).toBe('nova is free.');
  expect(input.hasAttribute('aria-invalid')).toBe(false);
  expect(document.querySelector('#org_name_status').classList.contains('ds-c-ok')).toBe(true);
});

test('sending the form shows the button working', () => {
  const form = page('nova');
  initOrgNew(form, {ask: async () => ({ok: true, message: ''}), schedule: () => 0, cancel: () => {}});
  form.dispatchEvent(new Event('submit'));
  const button = form.querySelector('[data-testid="org-create-submit"]');
  expect(button.classList.contains('is-loading')).toBe(true);
  expect(button.getAttribute('aria-disabled')).toBe('true');
});
