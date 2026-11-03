import {GET} from '../modules/fetch.js';

/** Shows the answer of the name check on its line. Only text is assigned. */
export function applyNameCheck(line, answer, input = document.getElementById('repo_name')) {
  if (!line) return;
  if (!answer) {
    line.hidden = true;
    return;
  }
  line.hidden = false;
  line.textContent = answer.ok ? (line.getAttribute('data-ok-text') || '') : String(answer.message ?? '');
  line.classList.toggle('is-err', !answer.ok);
  line.classList.toggle('is-ok', Boolean(answer.ok));
  input?.setAttribute('aria-invalid', answer.ok ? 'false' : 'true');
}

/**
 * Starts the visibility cards from the default of the owner chosen (an organization can set one), until the person
 * picks a visibility themselves.
 */
export function initOwnerVisibility(owner = document.getElementById('uid'), form = owner?.form) {
  if (!owner?.querySelector('option[data-default-visibility]') || !form) return;
  let touched = false;
  const cards = () => form.querySelectorAll('input[name="visibility"]');
  for (const card of cards()) {
    card.addEventListener('change', () => {
      touched = true;
    });
  }
  owner.addEventListener('change', () => {
    const want = owner.options[owner.selectedIndex]?.getAttribute('data-default-visibility');
    if (touched || !want) return;
    for (const card of cards()) {
      if (card.value === want && !card.disabled) card.checked = true;
    }
  });
}

let timer = 0;

/** Checks the name of a new repository 300 ms after the last key, for the owner chosen. */
export function initRepoNew() {
  initOwnerVisibility();
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
