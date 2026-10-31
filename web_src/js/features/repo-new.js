import {GET} from '../modules/fetch.js';

/** Shows the answer of the name check on its line. Only text is assigned. */
export function applyNameCheck(line, answer) {
  if (!line) return;
  if (!answer) {
    line.hidden = true;
    return;
  }
  line.hidden = false;
  line.textContent = answer.ok ? (line.getAttribute('data-ok-text') || '') : String(answer.message ?? '');
  line.classList.toggle('is-err', !answer.ok);
  line.classList.toggle('is-ok', Boolean(answer.ok));
  document.getElementById('repo_name')?.setAttribute('aria-invalid', answer.ok ? 'false' : 'true');
}

let timer = 0;

/** Checks the name of a new repository 300 ms after the last key, for the owner chosen. */
export function initRepoNew() {
  const input = document.getElementById('repo_name');
  const line = document.getElementById('repo_name_status');
  if (!input || !line) return;
  const run = async () => {
    const name = input.value.trim();
    if (!name) {
      applyNameCheck(line, null);
      return;
    }
    const q = new URLSearchParams({name, uid: document.getElementById('uid')?.value || ''});
    try {
      const res = await GET(`${line.getAttribute('data-check-url')}?${q}`, {headers: {Accept: 'application/json'}});
      if (!res.ok) return;
      const answer = await res.json();
      if (input.value.trim() === name) applyNameCheck(line, answer);
    } catch { /* the name check is a hint; the form still decides */ }
  };
  const later = () => {
    clearTimeout(timer);
    timer = setTimeout(run, 300);
  };
  input.addEventListener('input', later);
  document.getElementById('uid')?.addEventListener('change', later);
}
