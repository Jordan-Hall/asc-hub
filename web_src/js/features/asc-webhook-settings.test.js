import {saveProblem, initAscWebhookSettings} from './asc-webhook-settings.js';

const html = `<form data-wh-autosave action="/a/b/settings/hooks/1/settings">
  <input id="payload_url" name="payload_url" value="https://ci.example.org/hook">
  <p id="payload_url_err" hidden><span></span></p>
  <input id="secret" name="secret" type="password">
  <button type="button" data-wh-replace="secret">Replace</button>
  <select name="message_type"><option value="1" selected>m.notice</option></select>
  <input type="checkbox" name="asc_events" value="change.landed">
  <p data-wh-status data-saved="Saved."></p>
  <p data-wh-failed hidden><span>Not saved.</span></p>
</form>`;

const settle = () => new Promise((resolve) => setTimeout(resolve, 0));

test('saveProblem reads the answer of the settings endpoint', () => {
  expect(saveProblem(200, {ok: true})).toBe(null);
  expect(saveProblem(422, {field: 'payload_url', error: 'Use https.'})).toEqual({field: 'payload_url', error: 'Use https.'});
  expect(saveProblem(500, null)).toEqual({field: '', error: ''});
  expect(saveProblem(200, null)).toEqual({field: '', error: ''});
});

test('a change saves the form; a refused address shows its message under the field', async () => {
  document.body.innerHTML = html;
  const answers = [
    {status: 422, body: {field: 'payload_url', error: 'Use https.'}},
    {status: 200, body: {ok: true}},
  ];
  const sent = [];
  globalThis.fetch = vi.fn((url, opts) => {
    sent.push([new URL(url).pathname, opts.headers.get('accept'), opts.body.getAll('asc_events')]);
    const a = answers.shift();
    return Promise.resolve({status: a.status, json: () => Promise.resolve(a.body)});
  });
  initAscWebhookSettings();
  const box = document.querySelector('input[type="checkbox"]');
  box.checked = true;
  box.dispatchEvent(new Event('change', {bubbles: true}));
  await settle();
  expect(sent).toEqual([['/a/b/settings/hooks/1/settings', 'application/json', ['change.landed']]]);
  expect(document.getElementById('payload_url').getAttribute('aria-invalid')).toBe('true');
  expect(document.getElementById('payload_url_err').hidden).toBe(false);
  expect(document.querySelector('#payload_url_err span').textContent).toBe('Use https.');

  const secret = document.getElementById('secret');
  secret.value = 'new-secret';
  secret.dispatchEvent(new Event('change', {bubbles: true}));
  await settle();
  expect(document.getElementById('payload_url').hasAttribute('aria-invalid')).toBe(false);
  expect(document.getElementById('payload_url_err').hidden).toBe(true);
  expect(document.querySelector('[data-wh-status]').textContent).toBe('Saved.');
  expect(secret.value).toBe('');
  expect(secret.placeholder).toBe('••••••••••••');
});

test('a lost connection says it was not saved, and Replace focuses the secret', async () => {
  document.body.innerHTML = html;
  globalThis.fetch = vi.fn(() => Promise.reject(new Error('offline')));
  initAscWebhookSettings();
  document.querySelector('[data-wh-replace]').click();
  expect(document.activeElement.id).toBe('secret');
  document.querySelector('input[type="checkbox"]').dispatchEvent(new Event('change', {bubbles: true}));
  await settle();
  expect(document.querySelector('[data-wh-failed]').hidden).toBe(false);
  expect(document.querySelector('[data-wh-status]').textContent).toBe('');
});

test('a refused field without a message of its own marks it and says the form was not saved', async () => {
  document.body.innerHTML = html;
  globalThis.fetch = vi.fn(() => Promise.resolve({status: 422, json: () => Promise.resolve({field: 'message_type', error: 'Bad.'})}));
  initAscWebhookSettings();
  document.querySelector('select').dispatchEvent(new Event('change', {bubbles: true}));
  await settle();
  expect(document.querySelector('select').getAttribute('aria-invalid')).toBe('true');
  expect(document.querySelector('[data-wh-failed]').hidden).toBe(false);
});
