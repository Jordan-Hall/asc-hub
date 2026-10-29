import {GET} from '../modules/fetch.js';

// The live hint under the user name of the sign up form: "Your handle: @name" while typing, and on leaving the field
// whether the name is free. The block [data-signup-handle] holds the address of the check (data-url) and the texts
// (data-t-handle, data-t-ok, data-t-taken, data-t-invalid, data-t-reserved; %s is the name).

const validName = /^[\w.-]{1,40}$/;

export function handleState(name) {
  if (name === '') return 'empty';
  return validName.test(name) ? 'typing' : 'invalid';
}

function say(hint, key, name) {
  hint.textContent = (hint.getAttribute(`data-t-${key}`) ?? '').replace('%s', name);
  hint.classList.toggle('is-err', key === 'invalid' || key === 'taken' || key === 'reserved');
}

export function initAscSignup() {
  const hint = document.querySelector('[data-signup-handle]');
  const input = document.querySelector('#user_name');
  if (!hint || !input) return;
  const update = () => {
    const name = input.value.trim();
    const state = handleState(name);
    if (state === 'empty') hint.textContent = '';
    else say(hint, state === 'typing' ? 'handle' : 'invalid', name);
    input.toggleAttribute('data-invalid', state === 'invalid');
  };
  input.addEventListener('input', update);
  input.addEventListener('blur', async () => {
    const name = input.value.trim();
    if (handleState(name) !== 'typing') return update();
    try {
      const res = await GET(`${hint.getAttribute('data-url')}?name=${encodeURIComponent(name)}`);
      if (!res.ok) return;
      const {state} = await res.json();
      if (input.value.trim() === name) say(hint, state === 'ok' ? 'ok' : state, name);
    } catch {
      // no answer: the sign up itself says if the name is taken
    }
  });
  update();
}
