import $ from 'jquery';
import {POST} from '../modules/fetch.js';
import {showErrorToast} from '../modules/toast.js';

const focusable = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/** The keys of an open confirm dialog: Escape closes it (a click on the dimmer does not), and Tab stays inside it. */
export function dialogKeydown(e, dialog, close) {
  if (e.key === 'Escape') {
    e.preventDefault();
    e.stopPropagation();
    close();
    return;
  }
  if (e.key !== 'Tab') return;
  const list = Array.from(dialog.querySelectorAll(focusable)).filter((el) => !el.closest('[hidden]'));
  if (!list.length) return;
  const first = list[0];
  const last = list[list.length - 1];
  if (!dialog.contains(document.activeElement)) {
    e.preventDefault();
    (e.shiftKey ? last : first).focus();
  } else if (e.shiftKey && document.activeElement === first) {
    e.preventDefault();
    last.focus();
  } else if (!e.shiftKey && document.activeElement === last) {
    e.preventDefault();
    first.focus();
  }
}

/** The input listener bindConfirmName last added to each input, so that opening the dialog again replaces it. */
const confirmSyncs = new WeakMap();

/**
 * Makes a dialog with an input[data-confirm-name] ask for name typed: the input is emptied and the approve button
 * stays disabled until the input holds the name (case is ignored, as the server ignores it). Returns the input, or
 * null when the dialog does not ask.
 */
export function bindConfirmName(dialog, name) {
  const input = dialog.querySelector('input[data-confirm-name]');
  if (!input) return null;
  const ok = dialog.querySelector('.ok.button');
  const want = String(name ?? '').toLowerCase();
  const sync = () => {
    if (ok) ok.disabled = input.value.trim().toLowerCase() !== want;
  };
  input.value = '';
  input.removeEventListener('input', confirmSyncs.get(input));
  confirmSyncs.set(input, sync);
  input.addEventListener('input', sync);
  sync();
  return input;
}

/** Opens the modal a .delete-button names (data-modal-id) and, when approved, sends its form, its htmx request or a POST of its data. */
export function showDeletePopup(e) {
  e.preventDefault();
  const $this = $(this || e.target);
  const dataArray = $this.data();

  const modalID = $this[0].getAttribute('data-modal-id');
  if (!modalID) {
    throw new Error('This button does not specify which modal it wants to open.');
  }

  const $dialog = $(`#${modalID}`);
  $dialog.find('.name').text($this.data('name'));
  for (const [key, value] of Object.entries(dataArray)) {
    if (key && key.startsWith('data')) {
      $dialog.find(`.${key}`).text(value);
    }
  }

  const trigger = $this[0];
  const confirmName = bindConfirmName($dialog[0], $this.data('name'));
  const onKeydown = (ev) => dialogKeydown(ev, $dialog[0], () => $dialog.modal('hide'));
  $dialog.modal({
    closable: false,
    onVisible: () => {
      document.addEventListener('keydown', onKeydown, true);
      if (confirmName) confirmName.focus();
    },
    onHidden: () => {
      document.removeEventListener('keydown', onKeydown, true);
      if (trigger.isConnected) trigger.focus();
    },
    onApprove: async () => {
      if (confirmName && $dialog[0].querySelector('.ok.button')?.disabled) return false;
      if ($this.data('type') === 'form') {
        document.querySelector($this.data('form')).requestSubmit();
        return;
      }
      if ($this[0].getAttribute('hx-confirm')) {
        e.detail.issueRequest(true);
        return;
      }
      const postData = new FormData();
      for (const [key, value] of Object.entries(dataArray)) {
        if (key && key.startsWith('data')) {
          postData.append(key.slice(4), value);
        }
        if (key === 'id') {
          postData.append('id', value);
        }
      }

      if (confirmName) postData.append('confirm_name', confirmName.value.trim());

      const response = await POST($this.data('url'), {data: postData});
      if (response.ok) {
        const data = await response.json();
        window.location.href = data.redirect;
      } else if (confirmName) {
        const data = await response.json().catch(() => ({}));
        showErrorToast(data.errorMessage || response.statusText);
      }
    },
  }).modal('show');
}

/** Lets the .delete-button elements under root open their modal: for markup that replaced part of a page after it loaded. */
export function initDeleteButtons(root) {
  $(root).find('.delete-button').on('click', showDeletePopup);
}
