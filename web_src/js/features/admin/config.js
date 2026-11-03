import {showTemporaryTooltip} from '../../modules/tippy.js';
import {POST} from '../../modules/fetch.js';

const {appSubUrl} = window.config;

/** The words under a value setting: an error (the field is marked invalid) or a hint; an empty text hides both. */
export function showValueState(form, {error = '', hint = ''}) {
  const input = form.elements.value;
  const field = input.closest('.ds-field');
  const err = form.querySelector('.adm-value-state .ds-err-msg');
  const note = form.querySelector('.adm-value-state .ds-hint');
  err.querySelector('span').textContent = error;
  err.hidden = !error;
  note.textContent = hint;
  note.hidden = !hint;
  field?.classList.toggle('is-err', Boolean(error));
  input.classList.toggle('is-err', Boolean(error));
  if (error) {
    input.setAttribute('aria-invalid', 'true');
  } else {
    input.removeAttribute('aria-invalid');
  }
}

/** A value setting that is not saved yet says so; back at the saved value it says nothing. */
export function markUnsaved(form) {
  const input = form.elements.value;
  if (input.value === input.getAttribute('data-saved')) {
    showValueState(form, {});
    return false;
  }
  showValueState(form, {hint: form.getAttribute('data-unsaved')});
  return true;
}

/** Saves a value setting (the default agent budget). A refused value stays in the field with the hub's reason under it. */
export async function saveValueForm(form, post = POST) {
  const input = form.elements.value;
  if (input.value === input.getAttribute('data-saved') && !input.hasAttribute('aria-invalid')) return true;
  form.setAttribute('aria-busy', 'true');
  let error = '';
  try {
    const resp = await post(form.getAttribute('action'), {data: new URLSearchParams({value: input.value})});
    let json = {};
    try {
      json = await resp.json();
    } catch {}
    if (json.errorMessage) error = json.errorMessage;
    else if (!resp.ok) error = form.getAttribute('data-failed');
  } catch {
    error = form.getAttribute('data-failed');
  }
  form.removeAttribute('aria-busy');
  if (error) {
    showValueState(form, {error});
    return false;
  }
  input.setAttribute('data-saved', input.value);
  showValueState(form, {hint: form.getAttribute('data-saved-text')});
  return true;
}

export function initAdminConfigs() {
  const elAdminConfig = document.querySelector('.page-content.admin.config');
  if (!elAdminConfig) return;

  // a value setting saves when it is left or on Enter, and says while it is not saved yet
  for (const form of elAdminConfig.querySelectorAll('form.adm-value-form')) {
    form.addEventListener('submit', (e) => {
      e.preventDefault();
      saveValueForm(form);
    });
    form.elements.value.addEventListener('input', () => markUnsaved(form));
    form.elements.value.addEventListener('change', () => saveValueForm(form));
  }

  for (const el of elAdminConfig.querySelectorAll('input[type="checkbox"][data-config-dyn-key]')) {
    el.addEventListener('change', async () => {
      try {
        const resp = await POST(`${appSubUrl}/admin/config`, {
          data: new URLSearchParams({key: el.getAttribute('data-config-dyn-key'), value: el.checked}),
        });
        const json = await resp.json();
        if (json.errorMessage) throw new Error(json.errorMessage);
      } catch (ex) {
        showTemporaryTooltip(el, ex.toString());
        el.checked = !el.checked;
      }
    });
  }
}
