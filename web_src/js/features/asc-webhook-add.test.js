import {addressProblem, addOutcome, initAscWebhookAdd} from './asc-webhook-add.js';

const html = (http) => `<button type="button" data-wh-add-open>Add webhook</button>
<dialog id="wh-add-dlg"><form method="post" action="/a/b/settings/hooks/add" data-wh-add data-http="${http}">
  <input type="radio" name="kind" value="generic" checked>
  <input type="radio" name="kind" value="chat">
  <input type="radio" name="kind" value="agent">
  <p id="wha_kind_err" hidden><span></span></p>
  <div data-wh-add-for="generic chat">
    <input id="wha_payload_url" name="payload_url" type="url" required>
    <p id="wha_payload_url_err" hidden data-refused="Use https." data-invalid="Enter a full address."><span></span></p>
  </div>
  <div data-wh-add-for="agent" hidden>
    <input id="wha_agent" name="agent" required disabled>
    <p id="wha_agent_err" hidden><span></span></p>
  </div>
  <div data-wh-add-for="agent" hidden>
    <textarea id="wha_task" name="task" required disabled></textarea>
    <p id="wha_task_err" hidden><span></span></p>
  </div>
  <p data-wh-add-failed hidden><span>Not added.</span></p>
  <button type="button" data-wh-add-close>Cancel</button>
  <button type="submit" data-wh-add-submit>Add webhook</button>
</form></dialog>`;

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

function setup(http) {
  document.body.innerHTML = html(http);
  const dialog = document.querySelector('dialog');
  dialog.showModal = vi.fn(() => dialog.setAttribute('open', ''));
  dialog.close = vi.fn(() => {
    dialog.removeAttribute('open');
    dialog.dispatchEvent(new Event('close'));
  });
  initAscWebhookAdd();
  return dialog;
}

const type = (el, value) => {
  el.value = value;
  el.dispatchEvent(new Event('input', {bubbles: true}));
};

const pick = (kind) => {
  const radio = document.querySelector(`input[value="${kind}"]`);
  radio.checked = true;
  radio.dispatchEvent(new Event('change', {bubbles: true}));
};

const submit = () => document.querySelector('form').dispatchEvent(new Event('submit', {cancelable: true}));

test('addressProblem follows the transport rule of the hub', () => {
  expect(addressProblem('https://ci.example.org/hook', 'none')).toBe('');
  expect(addressProblem('http://ci.example.org/hook', 'none')).toBe('refused');
  expect(addressProblem('http://localhost:9000/hook', 'none')).toBe('refused');
  expect(addressProblem('http://localhost:9000/hook', 'local')).toBe('');
  expect(addressProblem('http://127.0.0.1/hook', 'local')).toBe('');
  expect(addressProblem('http://[::1]:8080/hook', 'local')).toBe('');
  expect(addressProblem('http://ci.example.org/hook', 'local')).toBe('refused');
  expect(addressProblem('http://ci.example.org/hook', 'any')).toBe('');
  expect(addressProblem('ci.example.org/hook', 'none')).toBe('invalid');
  expect(addressProblem('ftp://ci.example.org/hook', 'any')).toBe('invalid');
});

test('addOutcome reads the answer of the add endpoint', () => {
  expect(addOutcome(200, {redirect: '/a/b/settings/hooks/7'})).toEqual({redirect: '/a/b/settings/hooks/7'});
  expect(addOutcome(422, {field: 'agent', error: 'Bad name.'})).toEqual({field: 'agent', error: 'Bad name.'});
  expect(addOutcome(500, null)).toEqual({field: ''});
  expect(addOutcome(200, {})).toEqual({field: ''});
});

test('Add webhook opens the dialog; a refused http address says why and turns off Add webhook', () => {
  const dialog = setup('none');
  document.querySelector('[data-wh-add-open]').click();
  expect(dialog.showModal).toHaveBeenCalled();
  const url = document.querySelector('#wha_payload_url');
  const err = document.querySelector('#wha_payload_url_err');
  const add = document.querySelector('[data-wh-add-submit]');
  type(url, 'http://localhost:9000/hook');
  expect(err.hidden).toBe(false);
  expect(err.textContent).toBe('Use https.');
  expect(url.getAttribute('aria-invalid')).toBe('true');
  expect(add.disabled).toBe(true);
  type(url, 'https://ci.example.org/hook');
  expect(err.hidden).toBe(true);
  expect(add.disabled).toBe(false);
});

test('the agent kind shows its own fields and sends them instead of the address', async () => {
  setup('none');
  const sent = [];
  globalThis.fetch = vi.fn((url, opts) => {
    sent.push([new URL(url).pathname, opts.headers.get('accept'), Array.from(opts.body.keys())]);
    return Promise.resolve({status: 422, json: () => Promise.resolve({field: 'agent', error: 'Bad name.'})});
  });
  type(document.querySelector('#wha_payload_url'), 'http://ci.example.org/hook');
  pick('agent');
  expect(document.querySelector('[data-wh-add-for="generic chat"]').hidden).toBe(true);
  expect(document.querySelector('#wha_payload_url').disabled).toBe(true);
  expect(document.querySelector('[data-wh-add-submit]').disabled).toBe(false);
  document.querySelector('#wha_agent').value = 'Fixer!';
  document.querySelector('#wha_task').value = 'Fix the build.';
  submit();
  await settle();
  expect(sent).toEqual([['/a/b/settings/hooks/add', 'application/json', ['kind', 'agent', 'task']]]);
  const err = document.querySelector('#wha_agent_err');
  expect(err.hidden).toBe(false);
  expect(err.textContent).toBe('Bad name.');
});

test('an added hook opens its page; a lost connection says it was not added', async () => {
  setup('none');
  const assign = vi.fn();
  vi.stubGlobal('location', {...window.location, assign});
  globalThis.fetch = vi.fn(() => Promise.resolve({status: 200, json: () => Promise.resolve({redirect: '/a/b/settings/hooks/7'})}));
  type(document.querySelector('#wha_payload_url'), 'https://ci.example.org/hook');
  submit();
  await settle();
  expect(assign).toHaveBeenCalledWith('/a/b/settings/hooks/7');

  globalThis.fetch = vi.fn(() => Promise.reject(new Error('offline')));
  submit();
  await settle();
  expect(document.querySelector('[data-wh-add-failed]').hidden).toBe(false);
  vi.unstubAllGlobals();
});

test('an address that is not one is refused before sending; Cancel closes and clears the dialog', () => {
  const dialog = setup('none');
  globalThis.fetch = vi.fn();
  type(document.querySelector('#wha_payload_url'), 'ci.example.org');
  submit();
  expect(globalThis.fetch).not.toHaveBeenCalled();
  expect(document.querySelector('#wha_payload_url_err').textContent).toBe('Enter a full address.');
  pick('chat');
  document.querySelector('[data-wh-add-close]').click();
  expect(dialog.close).toHaveBeenCalled();
  expect(document.querySelector('input[value="generic"]').checked).toBe(true);
  expect(document.querySelector('#wha_payload_url_err').hidden).toBe(true);
});
