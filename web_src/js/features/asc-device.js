// The code field of "Authorise the CLI": the person types or pastes the code the terminal printed (WDJB-MJHT). The field
// shows capitals with the hyphen after four characters, whatever was typed (lower case, spaces, a pasted hyphen). The
// hub reads the code the same way on the server, so this is for the eye only.

const LENGTH = 8;

export function formatUserCode(raw) {
  const chars = String(raw).toUpperCase().replace(/[^A-Z0-9]/g, '').slice(0, LENGTH);
  return chars.length > 4 ? `${chars.slice(0, 4)}-${chars.slice(4)}` : chars;
}

export function initAscDevice() {
  const input = document.querySelector('.asc-device #user_code, form[action$="/login/device"] #user_code');
  if (!input) return;
  input.setAttribute('autocapitalize', 'characters');
  input.setAttribute('autocorrect', 'off');
  input.setAttribute('spellcheck', 'false');
  input.addEventListener('input', () => {
    const at = input.selectionStart ?? input.value.length;
    const before = formatUserCode(input.value.slice(0, at)).length;
    input.value = formatUserCode(input.value);
    input.setSelectionRange(before, before);
  });
  if (input.value) input.value = formatUserCode(input.value);
}
