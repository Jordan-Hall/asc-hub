// The New release dialog of the release list (board Releases; a bottom sheet on a phone). The New release buttons open it
// in place of the full page. On the first opening it asks asc for the proposed tag and the notes written from the changes
// that landed since the last release (GET releases/asc_draft): the tag field takes the proposed tag when it is empty, and
// "Write from the N landed changes" puts the notes in the text, after what is already there. The title follows the tag
// until it is edited. Without scripts the buttons open the full page.

import {GET} from '../modules/fetch.js';

export function proposedText(hint, draft) {
  if (!draft.proposed_tag) return '';
  if (!draft.latest_tag) return hint.getAttribute('data-text-first').replace('{tag}', draft.proposed_tag);
  return hint.getAttribute('data-text').replace('{tag}', draft.proposed_tag).replace('{latest}', draft.latest_tag);
}

export function writeLabel(button, changes) {
  if (changes === 1) return button.getAttribute('data-label-one');
  return button.getAttribute('data-label-other').replace('%d', String(changes));
}

export function withNotes(current, notes) {
  const text = current.trimEnd();
  return text ? `${text}\n\n${notes}` : notes;
}

function applyDraft(dialog, draft) {
  const tag = dialog.querySelector('[name="tag_name"]');
  const title = dialog.querySelector('[name="title"]');
  const hint = dialog.querySelector('[data-rel-new-proposed]');
  const write = dialog.querySelector('[data-rel-new-write]');
  const state = dialog.querySelector('[data-rel-new-state]');
  const text = proposedText(hint, draft);
  hint.textContent = text;
  hint.hidden = !text;
  if (draft.proposed_tag && !tag.value) {
    tag.value = draft.proposed_tag;
    if (!title.value) title.value = draft.proposed_tag;
  }
  if (draft.changes > 0 && draft.notes) {
    write.textContent = writeLabel(write, draft.changes);
    write.hidden = false;
    write.addEventListener('click', () => {
      const notes = dialog.querySelector('[name="content"]');
      notes.value = withNotes(notes.value, draft.notes);
      state.textContent = draft.truncated ? state.getAttribute('data-truncated') : '';
      notes.focus();
    });
  }
}

async function loadDraft(dialog) {
  const state = dialog.querySelector('[data-rel-new-state]');
  state.textContent = state.getAttribute('data-loading');
  try {
    const res = await GET(dialog.getAttribute('data-draft-url'));
    state.textContent = '';
    if (res.ok) {
      applyDraft(dialog, await res.json());
    } else if (res.status === 503) {
      state.textContent = state.getAttribute('data-failed');
    }
  } catch {
    state.textContent = state.getAttribute('data-failed');
  }
}

export function initAscReleaseNew(root = document) {
  const dialog = root.querySelector('dialog[data-rel-new]');
  if (!dialog || typeof dialog.showModal !== 'function') return;
  const tag = dialog.querySelector('[name="tag_name"]');
  const title = dialog.querySelector('[name="title"]');
  let titleEdited = false;
  title.addEventListener('input', () => {
    titleEdited = title.value !== '';
  });
  tag.addEventListener('input', () => {
    if (!titleEdited) title.value = tag.value;
  });
  let loaded = false;
  let opener = null;
  for (const button of root.querySelectorAll('[data-rel-new-open]')) {
    button.addEventListener('click', (e) => {
      if (e.ctrlKey || e.metaKey || e.shiftKey || e.button !== 0) return;
      e.preventDefault();
      opener = button;
      dialog.showModal();
      tag.focus();
      if (!loaded) {
        loaded = true;
        loadDraft(dialog);
      }
    });
  }
  dialog.querySelector('[data-rel-new-close]')?.addEventListener('click', () => dialog.close());
  dialog.addEventListener('close', () => opener?.focus());
}
