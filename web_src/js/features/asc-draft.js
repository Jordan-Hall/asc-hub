// "Draft from the diff" of a new change (board NewChange): a button with data-asc-draft and data-url fetches the draft that
// asc writes from base..head (the subjects, the files, the size, the risk) and puts it into the title and the description.
// Nothing is replaced without asking when the person has written something already.

export function applyDraft(draft, fields, confirmFn) {
  const {title, body} = fields;
  if (draft.nothing) return 'nothing';
  const written = (title?.value || '').trim() !== '' || (body?.value || '').trim() !== '';
  if (written && !confirmFn()) return 'kept';
  if (title && draft.title) title.value = draft.title;
  if (body) {
    body.value = draft.body || '';
    body.dispatchEvent(new Event('input', {bubbles: true}));
  }
  title?.dispatchEvent(new Event('input', {bubbles: true}));
  return 'applied';
}

export function initAscDraft() {
  for (const btn of document.querySelectorAll('[data-asc-draft]')) {
    const note = btn.parentElement?.querySelector('[data-asc-draft-note]');
    btn.addEventListener('click', async () => {
      btn.disabled = true;
      if (note) note.textContent = '';
      try {
        const resp = await fetch(btn.getAttribute('data-url'), {headers: {Accept: 'application/json'}, credentials: 'same-origin'});
        if (!resp.ok) throw new Error(String(resp.status));
        const draft = await resp.json();
        const form = btn.closest('form');
        const result = applyDraft(draft, {title: form?.querySelector('[name=title]'), body: form?.querySelector('[name=content]')}, () => window.confirm(btn.getAttribute('data-confirm') || ''));
        if (result === 'nothing' && note) note.textContent = btn.getAttribute('data-empty') || '';
      } catch {
        if (note) note.textContent = btn.getAttribute('data-failed') || '';
      } finally {
        btn.disabled = false;
      }
    });
  }
}
