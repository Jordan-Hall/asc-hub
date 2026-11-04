// Copyright 2026 The ASC Authors. All rights reserved.
// SPDX-License-Identifier: GPL-3.0-or-later

import {POST} from '../modules/fetch.js';
import {showModal} from '../modules/modal.ts';

// "New issue" on the issue list (board Issues) opens a dialog with the title and the description. The text is kept as a
// draft on the server by the draft saver of the form (asc-newchange-draft.js): when a field is left, with "Save as draft",
// and when the dialog closes. "Open full form" saves the draft first, so the new issue page opens with the same text.
// "Create issue" sends the form to the new issue endpoint; its error is shown in the dialog and the text stays. Without
// the script the button is a link to the new issue page.

function plainClick(e) {
  return e.button === 0 && !e.metaKey && !e.ctrlKey && !e.shiftKey && !e.altKey;
}

export function initRepoIssueDialog(root = document, saver = null, {navigate = (url) => window.location.assign(url)} = {}) {
  const link = root.querySelector('a[data-issue-dialog]');
  const dialog = link && document.getElementById(link.getAttribute('data-issue-dialog'));
  const form = dialog?.querySelector('form[data-issue-dialog-form]');
  if (!form) return null;
  const error = form.querySelector('[data-issue-dialog-error]');
  const submit = form.querySelector('[data-issue-dialog-submit]');
  const note = form.querySelector('[data-asc-ncd-note]');
  let sending = false;

  const showError = (text) => {
    if (!error) return;
    error.textContent = text;
    error.hidden = false;
    form.querySelector('[name=title]')?.focus();
  };

  link.addEventListener('click', (e) => {
    if (!plainClick(e)) return;
    e.preventDefault();
    showModal(dialog, undefined);
    form.querySelector('[name=title]')?.focus();
  });

  // the text is kept when the dialog is closed (Escape, the close button or a click outside)
  dialog.addEventListener('close', () => {
    if (!sending) saver?.save();
  });

  const full = form.querySelector('[data-issue-full]');
  full?.addEventListener('click', async (e) => {
    if (!plainClick(e)) {
      saver?.save();
      return;
    }
    e.preventDefault();
    await saver?.save();
    navigate(full.getAttribute('href'));
  });

  form.addEventListener('submit', async (e) => {
    e.preventDefault();
    if (sending) return;
    sending = true;
    if (submit) submit.disabled = true;
    if (error) error.hidden = true;
    if (note) note.textContent = form.getAttribute('data-creating') || '';
    // a save still running finishes first, so it cannot bring back the draft the new issue removes
    await saver?.save();
    let message = form.getAttribute('data-failed') || '';
    try {
      const resp = await POST(form.getAttribute('action'), {data: new FormData(form), headers: {'x-csrf-token': window.config?.csrfToken ?? ''}});
      const json = await resp.json().catch(() => ({}));
      if (resp.ok && json.redirect) {
        saver?.sync();
        navigate(json.redirect);
        return;
      }
      if (json.errorMessage) message = json.errorMessage;
    } catch {
      // shown below
    }
    if (note) note.textContent = '';
    showError(message);
    sending = false;
    if (submit) submit.disabled = false;
  });
  return dialog;
}
