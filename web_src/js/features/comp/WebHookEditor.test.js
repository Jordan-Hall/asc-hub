import {initCompWebHookEditor} from './WebHookEditor.js';

test('the content type of a generic hook is shown for a POST only', () => {
  document.body.innerHTML = `<div class="new webhook">
    <div class="ds-field"><select id="http_method"><option value="POST" selected>POST</option><option value="GET">GET</option></select></div>
    <div class="ds-field" id="ct"><select id="content_type"><option value="1" selected>application/json</option></select></div>
  </div>`;
  initCompWebHookEditor();
  const field = document.getElementById('ct');
  expect(field.classList.contains('tw-hidden')).toBe(false);
  const method = document.getElementById('http_method');
  method.value = 'GET';
  method.dispatchEvent(new Event('change'));
  expect(field.classList.contains('tw-hidden')).toBe(true);
  method.value = 'POST';
  method.dispatchEvent(new Event('change'));
  expect(field.classList.contains('tw-hidden')).toBe(false);
});

test('a hook without a method or a content type is left as it is', () => {
  document.body.innerHTML = `<div class="new webhook"><div class="ds-field"><input id="payload_url"></div></div>`;
  expect(() => initCompWebHookEditor()).not.toThrow();
});
