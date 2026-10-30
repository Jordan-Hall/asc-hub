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

// An address needs something before and after one @ and a dot in the part after it. The hub decides what it accepts;
// this only catches a slip before the form is sent.
export function emailLooksValid(value) {
  return /^[^\s@]+@[^\s@.]+(\.[^\s@.]+)+$/.test(value);
}

// The message under the email field once the person leaves it with something that is not an address; it goes away as
// soon as the field is edited. The text is data-t-email-invalid of the form.
function initEmailCheck() {
  const input = document.querySelector('form [data-signup-email], form input#email[type="email"]');
  const form = input?.closest('form');
  const text = form?.getAttribute('data-t-email-invalid');
  if (!input || !text) return;
  const field = input.closest('.ds-field');
  const clear = () => {
    field.querySelector('[data-email-error]')?.remove();
    input.removeAttribute('aria-invalid');
    input.classList.remove('is-err');
    input.removeAttribute('aria-describedby');
  };
  input.addEventListener('input', clear);
  input.addEventListener('blur', () => {
    clear();
    const value = input.value.trim();
    if (value === '' || emailLooksValid(value)) return;
    const message = document.createElement('div');
    message.className = 'ds-err-msg';
    message.id = 'email-error';
    message.setAttribute('data-email-error', '');
    message.textContent = text;
    field.append(message);
    input.setAttribute('aria-invalid', 'true');
    input.setAttribute('aria-describedby', message.id);
    input.classList.add('is-err');
  });
}

export function initAscSignup() {
  initEmailCheck();
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
