import {GET} from '../modules/fetch.js';
import {applyNameCheck} from './repo-new.js';

/** Checks a name field against its address after `delay` ms of no typing and shows the answer on the status line. */
export function initNameCheck(input, line, query, delay = 400) {
  if (!input || !line) return;
  let timer = 0;
  const run = async () => {
    const name = input.value.trim();
    if (!name || input.disabled) {
      applyNameCheck(line, null, input);
      return;
    }
    try {
      const res = await GET(`${line.getAttribute('data-check-url')}?${new URLSearchParams({...query(), name})}`, {headers: {Accept: 'application/json'}});
      if (!res.ok) return;
      const answer = await res.json();
      if (input.value.trim() === name) applyNameCheck(line, answer, input);
    } catch { /* the name check is a hint; the form still decides */ }
  };
  input.addEventListener('input', () => {
    clearTimeout(timer);
    timer = setTimeout(run, delay);
  });
}

/** The username field of the profile page. */
export function initUsernameCheck() {
  initNameCheck(document.getElementById('username'), document.getElementById('username_status'), () => ({}));
}
