import {markUnsaved, saveValueForm} from './config.js';

const page = `
<form class="adm-value-form" action="/admin/config?key=asc.default_budget" data-unsaved="Not saved yet." data-saved-text="Saved." data-failed="Not saved. Try again.">
  <div class="ds-field">
    <input class="ds-input" name="value" value="20" data-saved="20">
    <div class="adm-value-state">
      <div class="ds-err-msg" hidden><span></span></div>
      <div class="ds-hint" hidden></div>
    </div>
  </div>
</form>`;

const answer = (status, body) => new Response(JSON.stringify(body), {status, headers: {'Content-Type': 'application/json'}});
let form, input, err, hint;
beforeEach(() => {
  document.body.innerHTML = page;
  form = document.querySelector('form');
  input = form.elements.value;
  err = form.querySelector('.ds-err-msg');
  hint = form.querySelector('.ds-hint');
});

test('a changed value says it is not saved yet, and back at the saved value says nothing', () => {
  input.value = '25';
  expect(markUnsaved(form)).toBe(true);
  expect(hint.hidden).toBe(false);
  expect(hint.textContent).toBe('Not saved yet.');
  input.value = '20';
  expect(markUnsaved(form)).toBe(false);
  expect(hint.hidden).toBe(true);
});

test('a refused value stays in the field, marked invalid, with the reason under it', async () => {
  const post = vi.fn().mockResolvedValue(answer(400, {errorMessage: 'The amount must be zero or more.'}));
  input.value = '-5';
  expect(await saveValueForm(form, post)).toBe(false);
  expect(post).toHaveBeenCalledWith('/admin/config?key=asc.default_budget', expect.anything());
  expect(post.mock.calls[0][1].data.get('value')).toBe('-5');
  expect(err.hidden).toBe(false);
  expect(err.textContent).toBe('The amount must be zero or more.');
  expect(input.getAttribute('aria-invalid')).toBe('true');
  expect(input.closest('.ds-field').classList.contains('is-err')).toBe(true);
  expect(input.value).toBe('-5');
  expect(input.getAttribute('data-saved')).toBe('20');

  // put back to the saved value, it is sent again so the error clears with the server's word
  post.mockResolvedValue(answer(200, {}));
  input.value = '20';
  expect(await saveValueForm(form, post)).toBe(true);
  expect(err.hidden).toBe(true);
  expect(input.hasAttribute('aria-invalid')).toBe(false);
});

test('a saved value says so and becomes the saved value', async () => {
  const post = vi.fn().mockResolvedValue(answer(200, {}));
  input.value = '12.5';
  expect(await saveValueForm(form, post)).toBe(true);
  expect(input.getAttribute('data-saved')).toBe('12.5');
  expect(hint.textContent).toBe('Saved.');
  expect(err.hidden).toBe(true);
});

test('an unchanged value is not sent', async () => {
  const post = vi.fn();
  expect(await saveValueForm(form, post)).toBe(true);
  expect(post).not.toHaveBeenCalled();
});

test('a failed request or an answer without words gives the general sentence', async () => {
  input.value = '30';
  expect(await saveValueForm(form, vi.fn().mockRejectedValue(new Error('offline')))).toBe(false);
  expect(err.textContent).toBe('Not saved. Try again.');
  expect(await saveValueForm(form, vi.fn().mockResolvedValue(new Response('oops', {status: 502})))).toBe(false);
  expect(err.textContent).toBe('Not saved. Try again.');
});
