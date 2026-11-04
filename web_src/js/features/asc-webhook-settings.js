// The Settings tab of one webhook (templates/webhook/settings.tmpl) has no Save button: each field is saved when it
// changes. The form is sent in the background and the server answers JSON; a refused address shows its message under
// the field, a lost connection the message at the end of the form, and a save says "Saved." to a screen reader.
// Replace beside a stored secret puts the cursor in the secret field to type a new one. Without script the form keeps
// its Save button.

import {POST} from '../modules/fetch.js';

// The message to show for an answer of the settings endpoint: null when it saved, the field and text otherwise.
export function saveProblem(status, body) {
  if (status === 200 && body?.ok) return null;
  if (status === 422 && body?.field) return {field: body.field, error: String(body.error ?? '')};
  return {field: '', error: ''};
}

export function initAscWebhookSettings() {
  const form = document.querySelector('form[data-wh-autosave]');
  if (!form) return;
  const status = form.querySelector('[data-wh-status]');
  const failed = form.querySelector('[data-wh-failed]');
  const show = (problem) => {
    for (const input of form.querySelectorAll('[aria-invalid]')) input.removeAttribute('aria-invalid');
    for (const msg of form.querySelectorAll('[id$="_err"]')) msg.hidden = true;
    failed.hidden = problem === null || problem.field !== '';
    status.textContent = problem === null ? status.getAttribute('data-saved') : '';
    if (!problem?.field) return;
    const input = form.querySelector(`[name="${CSS.escape(problem.field)}"]`);
    const msg = form.querySelector(`#${CSS.escape(problem.field)}_err`);
    input?.setAttribute('aria-invalid', 'true');
    if (msg) {
      msg.querySelector('span').textContent = problem.error;
      msg.hidden = false;
    }
  };

  let pending = Promise.resolve();
  const save = () => {
    // a change made while a save runs is sent after it, so the last one wins
    pending = pending.then(async () => {
      if (!form.reportValidity()) return;
      try {
        const resp = await POST(form.action, {data: new FormData(form), headers: {accept: 'application/json'}});
        const body = await resp.json().catch(() => null);
        const problem = saveProblem(resp.status, body);
        show(problem);
        if (problem === null) {
          const secret = form.querySelector('input[name="secret"]');
          if (secret?.value) {
            secret.value = '';
            secret.placeholder = '••••••••••••';
          }
        }
      } catch {
        show({field: '', error: ''});
      }
    });
  };
  form.addEventListener('change', save);
  form.addEventListener('submit', (e) => {
    e.preventDefault();
    save();
  });
  for (const btn of form.querySelectorAll('[data-wh-replace]')) {
    btn.addEventListener('click', () => document.getElementById(btn.getAttribute('data-wh-replace'))?.focus());
  }
}
