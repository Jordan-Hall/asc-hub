// The Add webhook dialog of a hooks list (templates/repo/settings/webhook/add_dialog.tmpl). Add webhook opens it; the
// kind cards show the fields of the chosen kind (an address, or for an ASC agent hook the agent and its task) and turn
// off the others so they are not sent. The address is checked as it is typed: a plain http address that the hub
// refuses shows why and turns off Add webhook. The form is sent in the background; the server answers with the new
// hook's page, or with the field it refused and why.

import {POST} from '../modules/fetch.js';

const loopback = /^(localhost|127\.\d{1,3}\.\d{1,3}\.\d{1,3}|\[::1\])$/i;

// What is wrong with the payload address raw: '' when the hub saves it, 'invalid' when it is not a full http or https
// address, 'refused' for a plain http address the hub refuses. http is the form's data-http: "any" (no transport
// rule), "local" (http only for loopback) or "none".
export function addressProblem(raw, http) {
  let u;
  try {
    u = new URL(raw.trim());
  } catch {
    return 'invalid';
  }
  if (!u.host || (u.protocol !== 'https:' && u.protocol !== 'http:')) return 'invalid';
  if (u.protocol === 'https:' || http === 'any') return '';
  return http === 'local' && loopback.test(u.hostname) ? '' : 'refused';
}

// What to do with an answer of the add endpoint: {redirect} when the hook was added, {field, error} when the server
// refused a field, {field: ''} when the hook was not added for another reason.
export function addOutcome(status, body) {
  if (status === 200 && typeof body?.redirect === 'string') return {redirect: body.redirect};
  if (status === 422 && body?.field) return {field: body.field, error: String(body.error ?? '')};
  return {field: ''};
}

export function initAscWebhookAdd() {
  const dialog = document.querySelector('#wh-add-dlg');
  const form = dialog?.querySelector('form[data-wh-add]');
  if (!form) return;
  const http = form.getAttribute('data-http');
  const url = form.querySelector('input[name="payload_url"]');
  const urlErr = form.querySelector('#wha_payload_url_err');
  const submit = form.querySelector('[data-wh-add-submit]');
  const failed = form.querySelector('[data-wh-add-failed]');

  const showField = (field, text) => {
    const input = form.querySelector(`[name="${CSS.escape(field)}"]`);
    const msg = form.querySelector(`#wha_${CSS.escape(field)}_err`);
    if (!msg) {
      failed.hidden = false; // a field the dialog does not show, as the branch of an agent hook
      return;
    }
    input?.setAttribute('aria-invalid', 'true');
    msg.querySelector('span').textContent = text;
    msg.hidden = false;
    if (input && !input.disabled && input.type !== 'radio') input.focus();
  };
  const clear = () => {
    for (const input of form.querySelectorAll('[aria-invalid]')) input.removeAttribute('aria-invalid');
    for (const msg of form.querySelectorAll('[id$="_err"]')) msg.hidden = true;
    failed.hidden = true;
  };
  const kinds = Array.from(form.querySelectorAll('input[name="kind"]'));
  const checkedKind = () => kinds.find((r) => r.checked);
  const kind = () => checkedKind()?.value ?? 'generic';
  const sendsAddress = () => kind() !== 'agent';
  // the address problem worth saying while it is typed: only a refused http address, as an unfinished one is normal
  const checkAddress = () => {
    const problem = sendsAddress() && url.value.trim() !== '' ? addressProblem(url.value, http) : '';
    submit.disabled = problem !== '';
    if (problem === 'refused') {
      url.setAttribute('aria-invalid', 'true');
      urlErr.querySelector('span').textContent = urlErr.getAttribute('data-refused');
      urlErr.hidden = false;
    } else {
      url.removeAttribute('aria-invalid');
      urlErr.hidden = true;
    }
  };
  const showKind = () => {
    const k = kind();
    for (const field of form.querySelectorAll('[data-wh-add-for]')) {
      const on = field.getAttribute('data-wh-add-for').split(' ').includes(k);
      field.hidden = !on;
      for (const input of field.querySelectorAll('input, textarea')) input.disabled = !on;
    }
    clear();
    checkAddress();
  };

  for (const btn of document.querySelectorAll('[data-wh-add-open]')) {
    btn.addEventListener('click', () => {
      dialog.showModal();
      checkedKind()?.focus();
    });
  }
  for (const btn of form.querySelectorAll('[data-wh-add-close]')) {
    btn.addEventListener('click', () => dialog.close());
  }
  dialog.addEventListener('close', () => {
    form.reset();
    showKind();
  });
  form.addEventListener('change', (e) => {
    if (e.target.name === 'kind') showKind();
  });
  url.addEventListener('input', checkAddress);

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    clear();
    if (sendsAddress()) {
      const problem = addressProblem(url.value, http);
      if (problem) {
        showField('payload_url', urlErr.getAttribute(`data-${problem}`));
        return;
      }
    }
    for (const input of form.querySelectorAll('input:not([disabled]), textarea:not([disabled])')) {
      if (input.required && input.value.trim() === '') {
        input.setAttribute('aria-invalid', 'true');
        input.focus();
        return;
      }
    }
    submit.disabled = true;
    try {
      const resp = await POST(form.action, {data: new FormData(form), headers: {accept: 'application/json'}});
      const out = addOutcome(resp.status, await resp.json().catch(() => null));
      if (out.redirect) {
        window.location.assign(out.redirect);
        return;
      }
      if (out.field) showField(out.field, out.error);
      else failed.hidden = false;
    } catch {
      failed.hidden = false;
    }
    submit.disabled = false;
  });
  showKind();
}
