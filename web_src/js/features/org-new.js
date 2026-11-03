import {GET} from '../modules/fetch.js';

async function askName(url, name) {
  const res = await GET(`${url}?name=${encodeURIComponent(name)}`);
  if (!res.ok && res.status !== 403) throw new Error(`${url}: ${res.status}`);
  return res.json();
}

/** Draws the preview card of the new organization form from the display name, the handle and the visibility chosen. */
export function drawPreview(form) {
  const name = form.querySelector('#org_name').value.trim();
  const display = form.querySelector('#full_name')?.value.trim() ?? '';
  const shown = form.querySelector('[data-preview-name]');
  const handle = name || shown.getAttribute('data-empty');
  shown.textContent = display || handle;
  const first = display || name;
  form.querySelector('[data-preview-initial]').textContent = first ? Array.from(first)[0].toUpperCase() : '?';
  const url = form.querySelector('[data-preview-url]');
  url.textContent = `${url.getAttribute('data-base')}${handle}`;
  const vis = Array.from(form.querySelectorAll('input[name="visibility"]')).find((r) => r.checked);
  form.querySelector('[data-preview-visibility]').textContent = vis ? form.querySelector(`label[for="${vis.id}"]`)?.textContent ?? '' : '';
}

/** Shows the answer for a handle under the field: free in green, a problem in red and on the field itself. */
export function showCheck(form, {ok, message}) {
  const status = form.querySelector('#org_name_status');
  const input = form.querySelector('#org_name');
  status.textContent = message ?? '';
  status.classList.toggle('ds-c-ok', Boolean(ok && message));
  status.classList.toggle('ds-c-err', Boolean(!ok && message));
  input.classList.toggle('is-err', Boolean(!ok && message));
  if (!ok && message) input.setAttribute('aria-invalid', 'true');
  else input.removeAttribute('aria-invalid');
}

/**
 * The new organization form (templates/org/create.tmpl): the handle is checked as it is typed (a pause of `wait` ms,
 * and only the answer for the latest text counts), the preview follows the handle and the visibility, and the
 * create button shows that it is working once the form is sent.
 */
export function initOrgNew(form = document.querySelector('#org-new-form'), {ask = askName, wait = 300, schedule = setTimeout, cancel = clearTimeout} = {}) {
  if (!form) return;
  const input = form.querySelector('#org_name');
  const url = form.getAttribute('data-check-url');
  let timer = null;
  let latest = 0;

  const check = async () => {
    const name = input.value.trim();
    const mine = ++latest;
    if (!name) {
      showCheck(form, {ok: false, message: ''});
      return;
    }
    showCheck(form, {ok: false, message: ''});
    form.querySelector('#org_name_status').textContent = form.getAttribute('data-checking');
    let res;
    try {
      res = await ask(url, name);
    } catch {
      res = {ok: false, message: ''}; // the server checks again when the form is sent
    }
    if (mine === latest) showCheck(form, res);
  };

  input.addEventListener('input', () => {
    drawPreview(form);
    if (timer !== null) cancel(timer);
    timer = schedule(() => {
      timer = null;
      check();
    }, wait);
  });
  form.querySelector('#full_name')?.addEventListener('input', () => drawPreview(form));
  form.addEventListener('change', (e) => {
    if (e.target.name === 'visibility') drawPreview(form);
  });
  form.addEventListener('submit', () => {
    const button = form.querySelector('[data-testid="org-create-submit"]');
    button.classList.add('is-loading');
    button.setAttribute('aria-disabled', 'true');
  });
  drawPreview(form);
  if (input.value.trim() && !input.classList.contains('is-err')) check();
}
