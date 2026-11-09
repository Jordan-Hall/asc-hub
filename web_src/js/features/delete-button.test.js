import {bindConfirmName, dialogKeydown} from './delete-button.js';

let dialog, close, outside;
beforeEach(() => {
  document.body.innerHTML = `
    <button id="outside">Delete account</button>
    <div id="dialog">
      <div class="content"><p>Delete your account?</p></div>
      <div class="actions"><button id="cancel">Cancel</button><button id="ok">Delete account</button><button disabled>Off</button></div>
    </div>`;
  dialog = document.querySelector('#dialog');
  outside = document.querySelector('#outside');
  close = vi.fn();
});

const key = (k, shiftKey = false) => new KeyboardEvent('keydown', {key: k, shiftKey, cancelable: true});

test('Escape closes the dialog', () => {
  const e = key('Escape');
  dialogKeydown(e, dialog, close);
  expect(close).toHaveBeenCalledOnce();
  expect(e.defaultPrevented).toBe(true);
});

test('Tab from the last button goes back to the first, and Shift+Tab from the first to the last', () => {
  document.querySelector('#ok').focus();
  let e = key('Tab');
  dialogKeydown(e, dialog, close);
  expect(e.defaultPrevented).toBe(true);
  expect(document.activeElement.id).toBe('cancel');

  e = key('Tab', true);
  dialogKeydown(e, dialog, close);
  expect(document.activeElement.id).toBe('ok');
  expect(close).not.toHaveBeenCalled();
});

test('Tab between the buttons is left to the browser, and focus outside comes back in', () => {
  document.querySelector('#cancel').focus();
  const e = key('Tab');
  dialogKeydown(e, dialog, close);
  expect(e.defaultPrevented).toBe(false);

  outside.focus();
  dialogKeydown(key('Tab'), dialog, close);
  expect(document.activeElement.id).toBe('cancel');
});

test('a dialog that asks for the name keeps approve off until the name is typed', () => {
  document.body.innerHTML = `
    <div id="named">
      <input data-confirm-name value="left over">
      <div class="actions"><button class="cancel button">Cancel</button><button class="ok button">Delete</button></div>
    </div>`;
  const named = document.querySelector('#named');
  const ok = named.querySelector('.ok.button');
  const input = bindConfirmName(named, 'alice/Widgets');
  expect(input.value).toBe('');
  expect(ok.disabled).toBe(true);

  input.value = 'alice/widget';
  input.dispatchEvent(new Event('input'));
  expect(ok.disabled).toBe(true);

  input.value = ' Alice/widgets ';
  input.dispatchEvent(new Event('input'));
  expect(ok.disabled).toBe(false);

  bindConfirmName(named, 'bob/other');
  expect(input.value).toBe('');
  expect(ok.disabled).toBe(true);
});

test('a dialog that does not ask for the name is left alone', () => {
  expect(bindConfirmName(dialog, 'alice/widgets')).toBeNull();
  expect(document.querySelector('#ok').disabled).toBe(false);
});
