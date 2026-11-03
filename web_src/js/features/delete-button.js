import $ from 'jquery';
import {POST} from '../modules/fetch.js';

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

  $dialog.modal({
    closable: false,
    onApprove: async () => {
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

      const response = await POST($this.data('url'), {data: postData});
      if (response.ok) {
        const data = await response.json();
        window.location.href = data.redirect;
      }
    },
  }).modal('show');
}

/** Lets the .delete-button elements under root open their modal: for markup that replaced part of a page after it loaded. */
export function initDeleteButtons(root) {
  $(root).find('.delete-button').on('click', showDeletePopup);
}
