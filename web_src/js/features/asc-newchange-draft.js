// Draft of a new change (board NewChange): the title and the description of the open-change form are saved to the server
// when the person leaves a field and a second passes without another change of focus, and with "Save as draft". The
// server keeps them with the person's settings and fills the form with them the next time the same comparison is opened.
// Ctrl+Enter opens the change from the title (the description already does that through js-quick-submit).

import {POST} from '../modules/fetch.js';

export const AUTOSAVE_DELAY_MS = 1000;

// The text of the two fields that make a draft, as one string to compare with the text last saved.
export function draftSignature(form) {
  const title = form.querySelector('[name=title]')?.value ?? '';
  const body = form.querySelector('[name=content]')?.value ?? '';
  return `${title}\u0000${body}`;
}

// A saver that sends the form when its text differs from the text last saved. `send` takes a FormData and resolves with
// true when the server kept it. Calls while a send is running wait for it, so two requests never overtake each other.
export function createSaver(form, send) {
  let last = draftSignature(form);
  let running = Promise.resolve();
  return {
    last: () => last,
    // the form holds exactly what the server holds
    sync() {
      last = draftSignature(form);
    },
    save(force = false) {
      running = running.then(async () => {
        const sig = draftSignature(form);
        if (!force && sig === last) return 'unchanged';
        const data = new FormData();
        data.set('id', form.getAttribute('data-asc-ncd-id') || '');
        data.set('title', form.querySelector('[name=title]')?.value ?? '');
        data.set('content', form.querySelector('[name=content]')?.value ?? '');
        try {
          if (await send(data)) {
            last = sig;
            return 'saved';
          }
        } catch {
          // fall through: a failed save is shown, the text stays in the form
        }
        return 'failed';
      });
      return running;
    },
    async discard(sendDiscard) {
      running = running.then(async () => {
        try {
          if (await sendDiscard()) {
            return 'discarded';
          }
        } catch {
          // shown as failed below
        }
        return 'failed';
      });
      return running;
    },
  };
}

function post(url, data) {
  return POST(url, {data, headers: {'x-csrf-token': window.config?.csrfToken ?? ''}});
}

export function initAscNewChangeDraft(root = document, {delay = AUTOSAVE_DELAY_MS} = {}) {
  const form = root.querySelector('form[data-asc-ncd-url]');
  if (!form) return null;
  const url = form.getAttribute('data-asc-ncd-url');
  const note = form.querySelector('[data-asc-ncd-note]');
  const say = (key) => {
    if (note) note.textContent = note.getAttribute(`data-${key}`) || '';
  };
  const saver = createSaver(form, async (data) => (await post(url, data)).ok);
  let timer = 0;
  const later = () => {
    clearTimeout(timer);
    timer = setTimeout(async () => {
      const result = await saver.save();
      if (result === 'saved') say('saved');
      else if (result === 'failed') say('failed');
    }, delay);
  };
  for (const field of form.querySelectorAll('[name=title], [name=content]')) {
    field.addEventListener('blur', later);
  }
  form.querySelector('[data-asc-ncd-save]')?.addEventListener('click', async () => {
    clearTimeout(timer);
    const result = await saver.save(true);
    say(result === 'saved' ? 'saved' : 'failed');
  });
  form.querySelector('[data-asc-ncd-discard]')?.addEventListener('click', async (e) => {
    clearTimeout(timer);
    if (!window.confirm(e.currentTarget.getAttribute('data-confirm') || '')) return;
    const data = new FormData();
    data.set('id', form.getAttribute('data-asc-ncd-id') || '');
    data.set('discard', '1');
    const result = await saver.discard(async () => (await post(url, data)).ok);
    if (result === 'discarded') {
      for (const field of form.querySelectorAll('[name=title], [name=content]')) {
        field.value = '';
        field.dispatchEvent(new Event('input', {bubbles: true}));
      }
      saver.sync();
    }
    say(result === 'discarded' ? 'discarded' : 'failed');
  });
  // the draft is of no use once the change is being opened
  form.addEventListener('submit', () => clearTimeout(timer));
  return saver;
}
